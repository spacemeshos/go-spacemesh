import time
from tests import pod
from tests.fixtures import DeploymentInfo
from tests.deployment import create_deployment
from tests.misc import CoreV1ApiClient

CLIENT_POD_FILE = './k8s/single-client-w-conf.yml'
CLIENT_DEPLOYMENT_FILE = './k8s/client-w-conf.yml'


def add_single_client(deployment_id, namespace, container_specs):
    resp = pod.create_pod(CLIENT_POD_FILE,
                          namespace,
                          deployment_id=deployment_id,
                          container_specs=container_specs)

    client_name = resp.metadata.name
    print("Add new client: {0}".format(client_name))
    return client_name


def new_client_in_namespace(name_space, setup_bootstrap, cspec, num, t_out):
    resp = create_deployment(CLIENT_DEPLOYMENT_FILE, name_space,
                             deployment_id=setup_bootstrap.deployment_id,
                             replica_size=num,
                             container_specs=cspec,
                             time_out=t_out)
    client_info = DeploymentInfo(dep_id=setup_bootstrap.deployment_id)
    client_info.deployment_name = resp.metadata._name
    namespaced_pods = CoreV1ApiClient().list_namespaced_pod(namespace=name_space, include_uninitialized=True).items
    client_pods = list(filter(lambda i: i.metadata.name.startswith(client_info.deployment_name), namespaced_pods))

    client_info.pods = [{'name': c.metadata.name, 'pod_ip': c.status.pod_ip} for c in client_pods]
    print("Number of client pods: {0}".format(len(client_info.pods)))

    for c in client_info.pods:
        while True:
            resp = CoreV1ApiClient().read_namespaced_pod(name=c['name'], namespace=name_space)
            if resp.status.phase != 'Pending':
                break
            time.sleep(1)
    return client_info

