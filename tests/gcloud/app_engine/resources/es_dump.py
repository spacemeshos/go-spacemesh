import json
import requests
import time

SHIPPER = "fluent-bit"
INDX = SHIPPER+"-{namespace}-{index_date}"


class DstCountException(Exception):
    def __init__(self, dst_docs_count, elapsed_time):
        self.dst_docs_count = dst_docs_count
        self.elapsed_time = elapsed_time
        self.message = "destination documents count is stuck at {count} for {elapsed_time} minutes"
        super().__init__(self.message)

    def __str__(self):
        return self.message.format(count=self.dst_docs_count, elapsed_time=self.elapsed_time)


def wait_for_dump_to_end(src_ip, dst_ip, indx, usr, pwd, port=9200, timeout=86400):
    orig_timeout = timeout
    url = "http://{usr}:{pwd}@{ip}:{port}/{indx}/_stats"
    src_url = url.format(ip=src_ip, port=port, indx=indx, usr=usr, pwd=pwd)
    dst_url = url.format(ip=dst_ip, port=port, indx=indx, usr=usr, pwd=pwd)
    # get the source document count
    src_res = requests.get(src_url)
    src_docs_count = src_res.json()["indices"][indx]["primaries"]["docs"]["count"]
    print(f"source documents count: {src_docs_count}")
    # set parameters for while loop
    dst_docs_count = 0
    first_iter_dst_docs_count = 0
    mins_until_dst_validation = 9
    interval = 10
    while timeout >= 0:
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
            # if is first iteration then save the destination document count for later validation
            if timeout == orig_timeout - interval:
                first_iter_dst_docs_count = dst_docs_count
            print(f"destination's documents count: {dst_docs_count}")
        else:
            raise Exception(f"got a bad status code when sending GET {dst_url}\nstatus code: {dst_res.status_code}")
        # check if first iteration destination docs count equals to current after `mins_until_dst_validation` minutes
        mins_val_in_secs = 60 * mins_until_dst_validation
        if first_iter_dst_docs_count == dst_docs_count and orig_timeout - timeout > mins_val_in_secs:
            raise DstCountException(dst_docs_count, mins_until_dst_validation)

    # validate destination got all logs
    if src_docs_count > dst_docs_count and timeout <= 0:
        raise Exception(f"timed out while waiting for dump to finish!! timeout={orig_timeout}")


def es_reindex(namespace, index_date, es_ip, es_user, es_pass, main_es_ip, port=9200, timeout=60):
    """
    reindexing local ES data to the main ES server

    :param namespace: string, namespace name
    :param index_date: string, date of the index creation
    :param es_ip: string, the ip address of the local ES server
    :param es_user: string, user name for the local ES server
    :param es_pass: string, password for the local ES server
    :param main_es_ip: string, the ip address of the main ES server
    :param port: int, local ES port
    :param timeout: int, dumping timeout
    :return: boolean, True if succeeded False if failed
    """
    indx = INDX.format(namespace=namespace, index_date=index_date)
    dump_req_body = {
        "source": {
            "remote": {
                "host": f"http://{es_ip}:{port}",
                "username": es_user,
                "password": es_pass
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
    print(f"\ndumping index: {indx}, from: {es_ip}:{port}, to: {main_es_ip}:{port}")
    # wait_for_completion false means don't wait for reindex to return an answer
    post_url = f"http://{es_user}:{es_pass}@{main_es_ip}:{port}/_reindex?wait_for_completion=false"
    headers = {"Content-Type": "application/json"}
    try:
        res = requests.post(url=post_url, data=json.dumps(dump_req_body), headers=headers, timeout=timeout)
    except Exception as e:
        raise Exception(f"elk dumping POST has failed:\n{e}")
    try:
        wait_for_dump_to_end(es_ip, main_es_ip, indx, es_user, es_pass)
    except DstCountException as e:
        # this means that even if we get this exception we do delete the namespace
        # if we'd like to change this behaviour raise this exception
        return "failed"
    except Exception as e:
        raise Exception(f"got an exception while waiting for dumping to be done:\n{e}")
    return "success"
