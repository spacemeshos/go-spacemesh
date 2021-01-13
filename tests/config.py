import tests.utils as ut


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

ES_USER_LOCAL = ut.get_env("ES_USER")
ES_PASS_LOCAL = ut.get_env("ES_PASS")
MAIN_ES_IP = "35.227.190.1"
MAIN_ES_URL = f"{MAIN_ES_IP}:9200"


BOOTSTRAP_PORT = 7513
ORACLE_SERVER_PORT = 3030
POET_SERVER_PORT = 80
