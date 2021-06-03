from flask import Flask, request
import json

from gcloud_tasks.add_task_to_queue import create_google_cloud_task
from resources import nodepool_handler
from resources.convenience import str2bool, validate_params_in_dict
from resources.es_dump import es_reindex
from resources.k8s_handler import delete_namespace, remove_clusterrole_binding


DUMP_APP_ROUTE = "/namespace-teardown"


def validate_params(request_json):
    minimal_params = ["namespace", "is_delns", "is_dump", "project_id", "pool_name", "cluster_name", "node_pool_zone"]
    validate_params_in_dict(request_json, minimal_params)
    if not str2bool(request_json["is_dump"]):
        return
    dump_params = ["index_date", "es_ip", "es_user", "es_pass", "main_es_ip", "dump_queue_name", "dump_queue_zone"]
    validate_params_in_dict(request_json["dump_params"], dump_params)


# If `entrypoint` is not defined in app.yaml, App Engine will look for an app
# called `app` in `main.py`.
app = Flask(__name__)


@app.route('/', methods=['POST'])
def teardown():
    print(f"teardown")
    payload = request.get_data(as_text=True) or '(empty payload)'
    try:
        payload_dict = json.loads(payload)
        print("payload was converted to json successfully")
    except Exception as e:
        return f"could not load payload to json object:\n{e}"
    try:
        validate_params(payload_dict)
        print(f"params were validated successfully\n{payload_dict}")
    except ValueError as e:
        print(e)
        return str(e)
    namespace = payload_dict['namespace']
    project_id = payload_dict["project_id"]
    print(f"starting teardown process for {namespace}")
    # delete node pool
    try:
        nodepool_handler.remove_node_pool(project_id, payload_dict["pool_name"], payload_dict["cluster_name"],
                                          payload_dict["node_pool_zone"])
        print(f"node pool {payload_dict['pool_name']} was deleted successfully")
    except Exception as e:
        print(f"could not delete node pool {payload_dict['pool_name']}:\n{e}")
    # remove clusterrolebinding
    remove_clusterrole_binding(project_id, payload_dict["cluster_name"], payload_dict["node_pool_zone"], "fluent-bit",
                               f"fluent-bit-clusterrole-binding-{namespace}")
    if str2bool(payload_dict["is_dump"]):
        dump_queue_name = payload_dict["dump_params"]["dump_queue_name"]
        dump_queue_zone = payload_dict["dump_params"]["dump_queue_zone"]
        dump_queue_params = {"project_id": project_id, "queue_name": dump_queue_name, "queue_zone": dump_queue_zone}
        create_google_cloud_task(dump_queue_params, payload_dict, path=DUMP_APP_ROUTE, in_seconds=None,
                                 **payload_dict["dump_params"])
    elif str2bool(payload_dict["is_delns"]):
        try:
            delete_namespace(namespace, project_id, payload_dict["node_pool_zone"], payload_dict["cluster_name"])
            print(f"namespace {namespace} was deleted")
        except Exception as e:
            return f"failed deleting namespace {namespace}:\n{e}"
    else:
        print(f"did not delete namespace {namespace} please make sure to delete it after use")
    return "done"


@app.route(DUMP_APP_ROUTE, methods=['POST'])
def dump_and_delete_namespace():
    payload = request.get_data(as_text=True) or '(empty payload)'
    try:
        payload_dict = json.loads(payload)
        print("payload was converted to json successfully")
    except Exception as e:
        return f"could not load payload to json object:\n{e}"
    try:
        validate_params(payload_dict)
        print(f"params were validated successfully\n{payload_dict}")
    except ValueError as e:
        print(e)
        return str(e)
    namespace = payload_dict['namespace']
    try:
        print(f"starting dumping process for namespace - {namespace}")
        # dump ES logs to main ES server
        dump_dict = payload_dict["dump_params"]
        is_dumped = es_reindex(namespace, dump_dict['index_date'], dump_dict['es_ip'], dump_dict['es_user'],
                               dump_dict['es_pass'], dump_dict['main_es_ip'])
    except Exception as e:
        msg = f"failed dumping ES logs\n{e}"
        print(msg)
        return msg
    if is_dumped == "success" and payload_dict["is_delns"]:
        try:
            delete_namespace(namespace, payload_dict["project_id"], payload_dict["node_pool_zone"],
                             payload_dict["cluster_name"])
            print(f"namespace {namespace} was deleted")
        except Exception as e:
            return f"failed deleting namespace {namespace}:\n{e}"
    else:
        print(f"did not delete namespace {namespace} please make sure to delete it after use")
    return "done"
