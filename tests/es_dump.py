import tests.config as cnf
from tests.context import ES
from tests.utils import exec_wait


INDX = "filebeat-{namespace}-{filebeat_index_date}"
DMP_CMD = "elasticdump --input=http://{es_input_user}:{es_input_pass}@{es_ip}:9200/{index} " \
          "--output={file_name}.json --type={type} --limit={limit} --concurrency=50"
REST_CMD = "elasticdump --input={file_name}.json --concurrency=50 " \
           "--output=http://{es_output_user}:{es_output_pass}@34.82.60.199:9200/{index} --type={type} --limit={limit}"
DIRECT_CMD = "elasticdump --input=http://{es_input_user}:{es_input_pass}@{es_ip}:9200/{index} " \
             "--output=http://{es_output_user}:{es_output_pass}@34.82.60.199:9200/{index} --type={type} --limit={limit}"


def dump_es(namespace, filebeat_index_date, limit=500):
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


def restore_es(namespace, filebeat_index_date, limit=500):
    indx = INDX.format(namespace=namespace, filebeat_index_date=filebeat_index_date)
    mapping_file_name = indx + "-mapping"
    data_file_name = indx + "-data"
    es_out_ip = "34.82.60.199"

    rest_map = REST_CMD.format(file_name=mapping_file_name, es_output_user=cnf.ES_USER_LOCAL, es_output_pass=cnf.ES_PASS_LOCAL,
                               index=indx, type="mapping", limit=limit)
    rest_data = REST_CMD.format(file_name=data_file_name, es_output_user=cnf.ES_USER_LOCAL, es_output_pass=cnf.ES_PASS_LOCAL,
                                index=indx, type="data", limit=limit)

    exec_wait(rest_map)
    exec_wait(rest_data)


def dump_direct(namespace, filebeat_index_date, limit=500):
    indx = f"filebeat-{namespace}-{filebeat_index_date}"
    es_ip = ES(namespace).es_ip

    dump_map = DMP_CMD.format(es_input_user=cnf.ES_USER_LOCAL, es_input_pass=cnf.ES_PASS_LOCAL, es_ip=es_ip,
                              namespace=namespace, index=indx, es_output_user=cnf.ES_USER_LOCAL,
                              es_output_pass=cnf.ES_PASS_LOCAL, type="mapping", limit=limit)
    dump_data = DMP_CMD.format(es_input_user=cnf.ES_USER_LOCAL, es_input_pass=cnf.ES_PASS_LOCAL, es_ip=es_ip,
                               namespace=namespace, index=indx, es_output_user=cnf.ES_USER_LOCAL,
                               es_output_pass=cnf.ES_PASS_LOCAL, type="data", limit=limit)
    try:
        exec_wait(dump_map)
        exec_wait(dump_data)
    except Exception as e:
        print("failed dumping data\mapping")
        print(e)
