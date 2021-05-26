import datetime
from google.cloud import tasks_v2
import json

from tests import config as conf


def create_google_cloud_task(namespace, is_delns, is_dump, cluster_name, zone, in_seconds=None, **kwargs):
    # TODO: move to app engine fluent_bit_teardown
    # Create a client.
    client = tasks_v2.CloudTasksClient()
    # these params will be validated in case 'is_dump' var is True
    dump_params = ["index_date", "es_ip", "es_user", "es_pass", "main_es_ip"]
    project_id = conf.PROJECT_ID
    # TODO: should those vars be added to env?
    queue_name = 'namespace-teardown'
    location = 'us-east1'
    # notice: pool name is derived from the namespace - pool-NAMESPACE
    payload = {"namespace": namespace, "is_dump": is_dump, "project_id": project_id, "pool_name": f"pool-{namespace}",
               "cluster_name": cluster_name, "zone": zone, "is_delns": is_delns}
    if is_dump:
        for param in dump_params:
            if param not in kwargs:
                raise ValueError(f"missing {param} param in order to dump ES")
        # add dumping arguments to payload
        payload.update(kwargs)
    # Construct the fully qualified queue name.
    parent = client.queue_path(project_id, location, queue_name)

    # Construct the request body.
    task = {
        'app_engine_http_request': {  # Specify the type of request.
            'http_method': tasks_v2.HttpMethod.POST,
            'relative_uri': '/'
        }
    }
    if payload is not None:
        if isinstance(payload, dict):
            # Convert dict to JSON string
            payload = json.dumps(payload)
            # specify http content-type to application/json
            task["app_engine_http_request"]["headers"] = {"Content-type": "application/json"}
        # The API expects a payload of type bytes.
        converted_payload = payload.encode()

        # Add the payload to the request.
        task['app_engine_http_request']['body'] = converted_payload

    if in_seconds is not None:
        # Convert "seconds from now" into an rfc3339 datetime string.
        timestamp = datetime.datetime.utcnow() + datetime.timedelta(seconds=in_seconds)

        # Add the timestamp to the tasks.
        task['schedule_time'] = timestamp

    # Use the client to build and send the task.
    response = client.create_task(parent=parent, task=task)
    dir(response)
    print('Created task {}'.format(response.name))
    return response
