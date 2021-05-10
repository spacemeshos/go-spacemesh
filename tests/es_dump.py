import json
import requests
import time

import tests.config as cnf
from tests.context import ES
import tests.utils as ut


SHIPPER = "fluent-bit"
# correlates with logstash index format (under pipline-config.yml)
INDX = SHIPPER+"-{namespace}-{index_date}"
DMP_CMD = "elasticdump --input=http://{es_input_user}:{es_input_pass}@{es_ip}:9200/{index} " \
          "--output={file_name}.json --type={type} --limit={limit} --concurrency=50"
REST_CMD = "elasticdump --input={file_name}.json --concurrency=50 " \
           "--output=http://{es_output_user}:{es_output_pass}@"+cnf.MAIN_ES_URL+"/{index} --type={type} --limit={limit}"
DIRECT_CMD = "elasticdump --input=http://{es_input_user}:{es_input_pass}@{es_ip}:9200/{index} " \
             "--output=http://{es_output_user}:{es_output_pass}@"+cnf.MAIN_ES_URL+"/{index} --type={type} " \
             "--limit={limit}"


def elasticdump_to_file(namespace, index_date, limit=500):
    indx = INDX.format(namespace=namespace, index_date=index_date)
    mapping_file_name = indx + "-mapping"
    data_file_name = indx + "-data"
    es_ip = ES(namespace).es_ip
    dump_map = DMP_CMD.format(es_input_user=cnf.ES_USER_LOCAL, es_input_pass=cnf.ES_PASS_LOCAL, es_ip=es_ip,
                              index=indx, file_name=mapping_file_name, type="mapping", limit=limit)
    dump_data = DMP_CMD.format(es_input_user=cnf.ES_USER_LOCAL, es_input_pass=cnf.ES_PASS_LOCAL, es_ip=es_ip,
                               index=indx, file_name=data_file_name, type="data", limit=limit)
    ut.exec_wait(dump_map)
    ut.exec_wait(dump_data)


def elasticdump_from_file(namespace, index_date, limit=500):
    indx = INDX.format(namespace=namespace, index_date=index_date)
    mapping_file_name = indx + "-mapping"
    data_file_name = indx + "-data"
    rest_map = REST_CMD.format(file_name=mapping_file_name, es_output_user=cnf.ES_USER_LOCAL, es_output_pass=cnf.ES_PASS_LOCAL,
                               index=indx, type="mapping", limit=limit)
    rest_data = REST_CMD.format(file_name=data_file_name, es_output_user=cnf.ES_USER_LOCAL, es_output_pass=cnf.ES_PASS_LOCAL,
                                index=indx, type="data", limit=limit)
    ut.exec_wait(rest_map)
    ut.exec_wait(rest_data)


def elasticdump_direct(namespace, index_date, limit=500):
    indx = INDX.format(namespace=namespace, index_date=index_date)
    es_ip = ES(namespace).es_ip
    dump_map = DMP_CMD.format(es_input_user=cnf.ES_USER_LOCAL, es_input_pass=cnf.ES_PASS_LOCAL, es_ip=es_ip,
                              namespace=namespace, index=indx, es_output_user=cnf.ES_USER_LOCAL,
                              es_output_pass=cnf.ES_PASS_LOCAL, type="mapping", limit=limit)
    dump_data = DMP_CMD.format(es_input_user=cnf.ES_USER_LOCAL, es_input_pass=cnf.ES_PASS_LOCAL, es_ip=es_ip,
                               namespace=namespace, index=indx, es_output_user=cnf.ES_USER_LOCAL,
                               es_output_pass=cnf.ES_PASS_LOCAL, type="data", limit=limit)
    ut.exec_wait(dump_map)
    ut.exec_wait(dump_data)


def es_reindex(namespace, index_date, port=9200, timeout=1000):
    """
    reindexing local ES data to the main ES server

    :param namespace: string, namespace name
    :param index_date: string, date of the index creation
    :param port: int, local ES port
    :param timeout: int, dumping timeout
    :return: boolean, True if succeeded False if failed
    """
    indx = INDX.format(namespace=namespace, index_date=index_date)
    try:
        es_ip = ES(namespace).es_ip
    except Exception as e:
        print(f"failed getting local ES IP: {e}\ncannot reindex ES!!!")
        return False
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
    # wait_for_completion false means don't wait for reindex to return an answer
    post_url = f"http://{cnf.ES_USER_LOCAL}:{cnf.ES_PASS_LOCAL}@{cnf.MAIN_ES_URL}/_reindex?wait_for_completion=false"
    headers = {"Content-Type": "application/json"}
    try:
        requests.post(url=post_url, data=json.dumps(dump_req_body), headers=headers, timeout=timeout)
    except Exception as e:
        print(f"elk dumping POST has failed: {e}")
        return False
    try:
        _, time_waiting = wait_for_dump_to_end(es_ip, cnf.MAIN_ES_IP, indx)
        print(f"total time waiting:", time_waiting)
    except Exception as e:
        print(f"got an exception while waiting for dumping to be done: {e}")
        return False
    return True


@ut.timing
def wait_for_dump_to_end(src_ip, dst_ip, indx, port=9200, timeout=3600, usr=cnf.ES_USER_LOCAL, pwd=cnf.ES_PASS_LOCAL):
    orig_timeout = timeout
    url = "http://{usr}:{pwd}@{ip}:{port}/{indx}/_stats"
    src_url = url.format(ip=src_ip, port=port, indx=indx, usr=usr, pwd=pwd)
    dst_url = url.format(ip=dst_ip, port=port, indx=indx, usr=usr, pwd=pwd)
    src_res = requests.get(src_url)
    src_docs_count = src_res.json()["indices"][indx]["primaries"]["docs"]["count"]
    print(f"source documents count: {src_docs_count}")
    dst_docs_count = 0
    interval = 10
    while src_docs_count > dst_docs_count and timeout >= 0:
        # sleep first in order to allow index to be created at destination
        if src_docs_count > dst_docs_count:
            timeout -= interval
            time.sleep(interval)
        else:
            print(f"finished dumping logs, destinations' document count: {dst_docs_count}")
            break
        dst_res = requests.get(dst_url)
        if dst_res.status_code == 200:
            dst_docs_count = dst_res.json()["indices"][indx]["primaries"]["docs"]["count"]
            print(f"destinations' documents count: {dst_docs_count}")
        else:
            # TODO: handle exceptions more delicately with attendance to different err types (400, 401, 404..)
            #  maybe create a different function for GET/POST requests
            raise Exception(f"got a bad status code when sending GET {dst_url}\nstatus code: {dst_res.status_code}")
    # validate destination got all logs
    if src_docs_count > dst_docs_count and timeout <= 0:
        raise Exception(f"timed out while waiting for dump to finish!! timeout={orig_timeout}")
