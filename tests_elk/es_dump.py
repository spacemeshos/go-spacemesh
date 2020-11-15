import json
import requests
import time

import tests_elk.config as cnf
from tests_elk.context import ES
from tests_elk.utils import exec_wait


INDX = "filebeat-{namespace}-{filebeat_index_date}"
DMP_CMD = "elasticdump --input=http://{es_input_user}:{es_input_pass}@{es_ip}:9200/{index} " \
          "--output={file_name}.json --type={type} --limit={limit} --concurrency=50"
REST_CMD = "elasticdump --input={file_name}.json --concurrency=50 " \
           "--output=http://{es_output_user}:{es_output_pass}@"+cnf.MAIN_ES_URL+"/{index} --type={type} --limit={limit}"
DIRECT_CMD = "elasticdump --input=http://{es_input_user}:{es_input_pass}@{es_ip}:9200/{index} " \
             "--output=http://{es_output_user}:{es_output_pass}@"+cnf.MAIN_ES_URL+"/{index} --type={type} " \
             "--limit={limit}"


def elasticdump_to_file(namespace, filebeat_index_date, limit=500):
    indx = INDX.format(namespace=namespace, filebeat_index_date=filebeat_index_date)
    mapping_file_name = indx + "-mapping"
    data_file_name = indx + "-data"
    es_ip = ES(namespace).es_ip
    dump_map = DMP_CMD.format(es_input_user=cnf.ES_USER_LOCAL, es_input_pass=cnf.ES_PASS_LOCAL, es_ip=es_ip,
                              index=indx, file_name=mapping_file_name, type="mapping", limit=limit)
    dump_data = DMP_CMD.format(es_input_user=cnf.ES_USER_LOCAL, es_input_pass=cnf.ES_PASS_LOCAL, es_ip=es_ip,
                               index=indx, file_name=data_file_name, type="data", limit=limit)
    exec_wait(dump_map)
    exec_wait(dump_data)


def elasticdump_from_file(namespace, filebeat_index_date, limit=500):
    indx = INDX.format(namespace=namespace, filebeat_index_date=filebeat_index_date)
    mapping_file_name = indx + "-mapping"
    data_file_name = indx + "-data"
    rest_map = REST_CMD.format(file_name=mapping_file_name, es_output_user=cnf.ES_USER_LOCAL, es_output_pass=cnf.ES_PASS_LOCAL,
                               index=indx, type="mapping", limit=limit)
    rest_data = REST_CMD.format(file_name=data_file_name, es_output_user=cnf.ES_USER_LOCAL, es_output_pass=cnf.ES_PASS_LOCAL,
                                index=indx, type="data", limit=limit)
    exec_wait(rest_map)
    exec_wait(rest_data)


def elasticdump_direct(namespace, filebeat_index_date, limit=500):
    indx = INDX.format(namespace=namespace, filebeat_index_date=filebeat_index_date)
    es_ip = ES(namespace).es_ip
    dump_map = DMP_CMD.format(es_input_user=cnf.ES_USER_LOCAL, es_input_pass=cnf.ES_PASS_LOCAL, es_ip=es_ip,
                              namespace=namespace, index=indx, es_output_user=cnf.ES_USER_LOCAL,
                              es_output_pass=cnf.ES_PASS_LOCAL, type="mapping", limit=limit)
    dump_data = DMP_CMD.format(es_input_user=cnf.ES_USER_LOCAL, es_input_pass=cnf.ES_PASS_LOCAL, es_ip=es_ip,
                               namespace=namespace, index=indx, es_output_user=cnf.ES_USER_LOCAL,
                               es_output_pass=cnf.ES_PASS_LOCAL, type="data", limit=limit)
    exec_wait(dump_map)
    exec_wait(dump_data)


def es_reindex(namespace, filebeat_index_date, port=9200, retry=3):
    indx = INDX.format(namespace=namespace, filebeat_index_date=filebeat_index_date)
    try:
        es_ip = ES(namespace).es_ip
    except Exception as e:
        print(f"failed getting local ES IP: {e}")
        return

    dump_req_body = {
        "source": {
            "remote": {
                "host": f"http://{es_ip}:{port}",
                "username": cnf.ES_USER_LOCAL,
                "password": cnf.ES_PASS_LOCAL
            },
            "index": indx,
            "query": {
                "match_all": {}
            }
        },
        "dest": {
            "index": indx
        }
    }
    print(f"\ndumping index: {indx}, from: {es_ip}:{port}, to: {cnf.MAIN_ES_URL}")
    post_url = f"http://{cnf.ES_USER_LOCAL}:{cnf.ES_PASS_LOCAL}@{cnf.MAIN_ES_URL}/_reindex"
    headers = {"Content-Type": "application/json"}
    try:
        res = requests.post(url=post_url, data=json.dumps(dump_req_body), headers=headers, timeout=480)
    except Exception as e:
        print(f"elk dumping POST has failed: {e}")
        return
    # got empty response
    if not res:
        print("response is empty!")
        return
    # valid response
    res_json = res.json()
    if res_json and res_json["timed_out"]:
        print("timed out while dumping data to main ES server")
        print(f"retrying ({retry} retries left)")
        time.sleep(1)
        es_reindex(namespace, filebeat_index_date, retry=retry-1)
    elif res_json and res_json["failures"]:
        print(f"found failures while dumping data to main ES server:", res_json["failures"])
        print(f"retrying ({retry} retries left)")
        time.sleep(1)
        es_reindex(namespace, filebeat_index_date, retry=retry-1)
    elif res_json:
        print(f"response:\n{json.dumps(res_json)}")
        print("done dumping")
