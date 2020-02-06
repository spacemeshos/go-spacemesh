from datetime import datetime, timedelta

import pytz
from kubernetes import client
from kubernetes.client import Configuration, ApiClient
#from pytest_testconfig import config as testconfig
from tests import deployment, statefulset



class K8SApiClient(ApiClient):

    def __init__(self):
        client_config = Configuration()
        client_config.connection_pool_maxsize = 32
        super(K8SApiClient, self).__init__(configuration=client_config)


class CoreV1ApiClient(client.CoreV1Api):

    def __init__(self):
        super(CoreV1ApiClient, self).__init__(api_client=K8SApiClient())


class ContainerSpec:

    REPLACEABLE_ARGS = ['randcon', 'oracle_server', 'bootnodes', 'genesis_time', 'poet_server']

    def __init__(self, cname, specs):
        self.name = cname
        self.image = specs['image']
        self.entrypoint = [specs['command']]
        self.resources = None if 'resources' not in specs else specs['resources']
        self.args = {}

    def append_args(self, **kwargs):
        self.args.update(kwargs)

    def update_deployment(self, dep):
        if dep['kind'] == 'Pod':
            containers = dep['spec']['containers']
        else:
            containers = dep['spec']['template']['spec']['containers']
        for c in containers:
            if c['name'] == self.name:
                # update the container specs
                if self.image:
                    c['image'] = self.image
                if self.entrypoint:
                    c['command'] = self.entrypoint
                if self.resources:
                    c['resources'] = self.resources
                c['args'] = self._update_args(c['args'], **(self.args))
                break
        return dep

    @staticmethod
    def _update_args(args_input_yaml, **kwargs):
        for k in kwargs:
            replaced = False
            if k in ContainerSpec.REPLACEABLE_ARGS:
                for i, arg in enumerate(args_input_yaml):
                    if arg[2:].replace('-', '_') == k:
                        # replace the value
                        args_input_yaml[i + 1] = kwargs[k]
                        replaced = True
                        break
            if not replaced:
                args_input_yaml += (['--{0}'.format(k.replace('_', '-')), kwargs[k]])
        return args_input_yaml


BOOT_DEPLOYMENT_FILE = './k8s/bootstrapoet-w-conf.yml'
BOOT_STATEFULSET_FILE = './k8s/bootstrapoet-w-conf-ss.yml'
CLIENT_DEPLOYMENT_FILE = './k8s/client-w-conf.yml'
CLIENT_STATEFULSET_FILE = './k8s/client-w-conf-ss.yml'
CURL_POD_FILE = './k8s/curl.yml'

BOOTSTRAP_PORT = 7513
ORACLE_SERVER_PORT = 3030
POET_SERVER_PORT = 50002
#GENESIS_TIME = pytz.utc.localize(datetime.utcnow() + timedelta(seconds=testconfig['genesis_delta']))


def get_conf(genesis_time, bs_info, client_config, setup_oracle=None, setup_poet=None, args=None):
    """
    get_conf gather specification information into one ContainerSpec object

    :param bs_info: DeploymentInfo, bootstrap info
    :param client_config: DeploymentInfo, client info
    :param setup_oracle: string, oracle ip
    :param setup_poet: string, poet ip
    :param args: list of strings, arguments for appendage in specification
    :return: ContainerSpec
    """
    client_args = {} if 'args' not in client_config else client_config['args']
    # append client arguments
    if args is not None:
        for arg in args:
            client_args[arg] = args[arg]

    # create a new container spec with client configuration
    cspec = ContainerSpec(cname='client', specs=client_config)

    # append oracle configuration
    if setup_oracle:
        client_args['oracle_server'] = 'http://{0}:{1}'.format(setup_oracle, ORACLE_SERVER_PORT)

    # append poet configuration
    if setup_poet:
        client_args['poet_server'] = '{0}:{1}'.format(setup_poet, POET_SERVER_PORT)

    cspec.append_args(bootnodes=node_string(bs_info['key'], bs_info['pod_ip'], BOOTSTRAP_PORT, BOOTSTRAP_PORT),
                      genesis_time=genesis_time)
                      #genesis_time=GENESIS_TIME.isoformat('T', 'seconds'))
    # append client config to ContainerSpec
    if len(client_args) > 0:
        cspec.append_args(**client_args)
    return cspec


def choose_k8s_object_create(config, deployment_file, statefulset_file):
    dep_type = 'deployment' if 'deployment_type' not in config else config['deployment_type']
    if dep_type == 'deployment':
        return deployment_file, deployment.create_deployment
    elif dep_type == 'statefulset':
        # StatefulSets are intended to be used with stateful applications and distributed systems.
        # Pods in a StatefulSet have a unique ordinal index and a stable network identity.
        return statefulset_file, statefulset.create_statefulset
    else:
        raise Exception("Unknown deployment type in configuration. Please check your config.yaml")


def add_multi_clients(testconfig, deployment_id, container_specs, size=2, client_title='client', ret_pods=False):
    """
    adds pods to a given namespace according to specification params

    :param deployment_id: string, namespace id
    :param container_specs:
    :param size: int, number of replicas
    :param client_title: string, client title in yml file (client, client_v2 etc)
    :param ret_pods: boolean, if 'True' RETURN a pods list (V1PodList)
    :return: list (strings), list of pods names
    """
    k8s_file, k8s_create_func = choose_k8s_object_create(testconfig[client_title],
                                                         CLIENT_DEPLOYMENT_FILE,
                                                         CLIENT_STATEFULSET_FILE)
    resp = k8s_create_func(k8s_file, testconfig['namespace'],
                           deployment_id=deployment_id,
                           replica_size=size,
                           container_specs=container_specs,
                           time_out=testconfig['deployment_ready_time_out'])

    print("\nadding new clients")
    client_pods = CoreV1ApiClient().list_namespaced_pod(testconfig['namespace'],
                                                        include_uninitialized=True,
                                                        label_selector=("name={0}".format(
                                                            resp.metadata._name.split('-')[1]))).items

    if ret_pods:
        ret_val = client_pods
    else:
        pods = []
        for c in client_pods:
            pod_name = c.metadata.name
            if pod_name.startswith(resp.metadata.name):
                pods.append(pod_name)
        ret_val = pods
        print(f"added new clients: {pods}\n")

    return ret_val
