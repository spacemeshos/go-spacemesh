import time

from tests import config as conf
from tests.pod import search_phrase_in_pod_log
from tests.misc import CoreV1ApiClient, ContainerSpec
from tests.utils import get_conf, get_genesis_time_delta, choose_k8s_object_create


def add_multi_clients(testconfig, deployment_id, container_specs, size=2, client_title='client', ret_dep=False):
    """
    adds pods to a given namespace according to specification params

    :param testconfig: dictionary, current run configuration
    :param deployment_id: string, namespace id
    :param container_specs:
    :param size: int, number of replicas
    :param client_title: string, client title in yml file (client, client_v2 etc)
    :param ret_dep: boolean, if 'True' RETURN deployment name in addition
    :return: list (strings), list of pods names
    """
    k8s_file, k8s_create_func = choose_k8s_object_create(testconfig[client_title],
                                                         conf.CLIENT_DEPLOYMENT_FILE,
                                                         conf.CLIENT_STATEFULSET_FILE)
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
    pods = []
    for c in client_pods:
        pod_name = c.metadata.name
        if pod_name.startswith(resp.metadata.name):
            pods.append(pod_name)
    ret_val = pods
    print(f"added new clients: {pods}\n")

    if ret_dep:
        ret_val = pods, resp.metadata._name

    return ret_val


def _setup_dep_ss_file_path(file_path, dep_type, node_type):
    # TODO we should modify this func to be a generic one
    """
    sets up deployment files
    :param file_path: string, a file path to overwrite the default file path value (BOOT_DEPLOYMENT_FILE,
    CLIENT_STATEFULSET_FILE, etc)
    :param dep_type: string, stateful or deployment
    :param node_type: string, client or bootstrap

    :return: string, string, deployment and stateful specification files paths
    """
    if node_type == 'bootstrap':
        dep_file_path = conf.BOOT_DEPLOYMENT_FILE
        ss_file_path = conf.BOOT_STATEFULSET_FILE
    elif node_type == 'client':
        dep_file_path = conf.CLIENT_DEPLOYMENT_FILE
        ss_file_path = conf.CLIENT_STATEFULSET_FILE
    else:
        raise ValueError(f"can not recognize node name: {node_type}")

    if file_path and dep_type == "statefulset":
        print(f"setting up stateful file path to {file_path}\n")
        ss_file_path = file_path
    elif file_path and dep_type == "deployment":
        print(f"setting up deployment file path to {file_path}\n")
        dep_file_path = file_path

    return dep_file_path, ss_file_path


def setup_bootstrap_in_namespace(namespace, bs_deployment_info, bootstrap_config, gen_time_del, oracle=None, poet=None,
                                 file_path=None, dep_time_out=120):
    """
    adds a bootstrap node to a specific namespace

    :param namespace: string, session id
    :param bs_deployment_info: DeploymentInfo, bootstrap info, metadata
    :param bootstrap_config: dictionary, bootstrap specifications
    :param gen_time_del: string, genesis time as set in suite specification file
    :param oracle: string, oracle ip
    :param poet: string, poet ip
    :param file_path: string, optional, full path to deployment yaml
    :param dep_time_out: int, deployment timeout

    :return: DeploymentInfo, bootstrap info with a list of active pods
    """
    # setting stateful and deployment configuration files
    dep_method = bootstrap_config["deployment_type"] if "deployment_type" in bootstrap_config.keys() else "deployment"
    try:
        dep_file_path, ss_file_path = _setup_dep_ss_file_path(file_path, dep_method, 'bootstrap')
    except ValueError as e:
        print(f"error setting up bootstrap specification file: {e}")
        return None

    def _extract_label():
        name = bs_deployment_info.deployment_name.split('-')[1]
        return name

    bootstrap_args = {} if 'args' not in bootstrap_config else bootstrap_config['args']
    cspec = ContainerSpec(cname='bootstrap', specs=bootstrap_config)

    if oracle:
        bootstrap_args['oracle_server'] = 'http://{0}:{1}'.format(oracle, conf.ORACLE_SERVER_PORT)

    if poet:
        bootstrap_args['poet_server'] = '{0}:{1}'.format(poet, conf.POET_SERVER_PORT)

    gen_delta = get_genesis_time_delta(gen_time_del)
    cspec.append_args(genesis_time=gen_delta.isoformat('T', 'seconds'),
                      **bootstrap_args)

    # choose k8s creation function (deployment/stateful) and the matching k8s file
    k8s_file, k8s_create_func = choose_k8s_object_create(bootstrap_config,
                                                         dep_file_path,
                                                         ss_file_path)
    # run the chosen creation function
    resp = k8s_create_func(k8s_file, namespace,
                           deployment_id=bs_deployment_info.deployment_id,
                           replica_size=bootstrap_config['replicas'],
                           container_specs=cspec,
                           time_out=dep_time_out)

    bs_deployment_info.deployment_name = resp.metadata._name
    # The tests assume we deploy only 1 bootstrap
    bootstrap_pod_json = (
        CoreV1ApiClient().list_namespaced_pod(namespace=namespace,
                                              label_selector=(
                                                  "name={0}".format(
                                                      _extract_label()))).items[0])
    bs_pod = {'name': bootstrap_pod_json.metadata.name}

    while True:
        resp = CoreV1ApiClient().read_namespaced_pod(name=bs_pod['name'], namespace=namespace)
        if resp.status.phase != 'Pending':
            break
        time.sleep(1)

    bs_pod['pod_ip'] = resp.status.pod_ip

    match = search_phrase_in_pod_log(bs_pod['name'], namespace, 'bootstrap',
                                     r"Local node identity >> (?P<bootstrap_key>\w+)")

    if not match:
        raise Exception("Failed to read container logs in {0}".format('bootstrap'))

    bs_pod['key'] = match.group('bootstrap_key')
    bs_deployment_info.pods = [bs_pod]

    return bs_deployment_info


def setup_clients_in_namespace(namespace, bs_deployment_info, client_deployment_info, client_config, genesis_time,
                               name="client", file_path=None, oracle=None, poet=None, dep_time_out=120):
    # setting stateful and deployment configuration files
    # default deployment method is 'deployment'
    dep_method = client_config["deployment_type"] if "deployment_type" in client_config.keys() else "deployment"
    try:
        dep_file_path, ss_file_path = _setup_dep_ss_file_path(file_path, dep_method, 'client')
    except ValueError as e:
        print(f"error setting up client specification file: {e}")
        return None

    # this function used to be the way to extract the client title
    # in case we want a different title (client_v2 for example) we can specify it
    # directly in "name" input
    def _extract_label():
        return client_deployment_info.deployment_name.split('-')[1]

    cspec = get_conf(bs_deployment_info, client_config, genesis_time, setup_oracle=oracle,
                     setup_poet=poet)

    k8s_file, k8s_create_func = choose_k8s_object_create(client_config, dep_file_path, ss_file_path)
    resp = k8s_create_func(k8s_file, namespace,
                           deployment_id=client_deployment_info.deployment_id,
                           replica_size=client_config['replicas'],
                           container_specs=cspec,
                           time_out=dep_time_out)

    dep_name = resp.metadata._name
    client_deployment_info.deployment_name = dep_name

    client_pods = (
        CoreV1ApiClient().list_namespaced_pod(namespace,
                                              include_uninitialized=True,
                                              label_selector=("name={0}".format(name))
                                              ).items
    )

    client_deployment_info.pods = [{'name': c.metadata.name, 'pod_ip': c.status.pod_ip} for c in client_pods
                                   if c.metadata.name.startswith(dep_name)]
    return client_deployment_info
