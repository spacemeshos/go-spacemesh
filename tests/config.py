import os


def get_es_usr():
    if "ES_USER" not in os.environ:
        raise Exception("ES_USER environment variable must be set")
    return os.getenv("ES_USER")


def get_es_password():
    if "ES_PASS" not in os.environ:
        raise Exception("ES_PASS environment variable must be set")
    return os.getenv("ES_PASS")


BOOT_DEPLOYMENT_FILE = './k8s/bootstrapoet-w-conf.yml'
BOOT_STATEFULSET_FILE = './k8s/bootstrapoet-w-conf-ss.yml'
CLIENT_DEPLOYMENT_FILE = './k8s/client-w-conf.yml'
CLIENT_STATEFULSET_FILE = './k8s/client-w-conf-ss.yml'
CURL_POD_FILE = './k8s/curl.yml'
ELASTIC_CONF_DIR = './elk/elastic/'
FILEBEAT_CONF_DIR = './elk/filebeat/'
FLUENT_BIT_CONF_DIR = './elk/fluent-bit/'
KIBANA_CONF_DIR = './elk/kibana/'
LOGSTASH_CONF_DIR = './elk/logstash/'

ES_USER_LOCAL = get_es_usr()
ES_PASS_LOCAL = get_es_password()
MAIN_ES_IP = "35.197.82.152"
MAIN_ES_URL = f"{MAIN_ES_IP}:9200"


BOOTSTRAP_PORT = 7513
ORACLE_SERVER_PORT = 3030
POET_SERVER_PORT = 80
