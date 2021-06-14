from google.api_core.exceptions import NotFound
from google.auth import compute_engine, transport
from google.cloud.container_v1 import ClusterManagerClient
from kubernetes import client


class K8SApiClient(client.ApiClient):
    def __init__(self, project_id, zone, cluster_id):
        self.project_id = project_id
        self.zone = zone
        self.cluster_id = cluster_id
        self.credentials = self.set_credentials()
        self.token = self.credentials.token
        client_config = client.Configuration()
        cluster_ip = self.get_cluster_ip()
        client_config.host = f"https://{cluster_ip}:443"
        client_config.verify_ssl = False
        client_config.api_key = {"authorization": "Bearer " + self.token}
        client_config.connection_pool_maxsize = 32
        super(K8SApiClient, self).__init__(configuration=client_config)

    def get_cluster_ip(self):
        cluster_manager_client = ClusterManagerClient(credentials=self.credentials)
        cluster = cluster_manager_client.get_cluster(
            name=f'projects/{self.project_id}/locations/{self.zone}/clusters/{self.cluster_id}'
        )
        return cluster.endpoint

    @staticmethod
    def set_credentials():
        credentials = compute_engine.Credentials()
        credentials.refresh(transport.requests.Request())
        return credentials


def delete_namespace(namespace, project_id, zone, cluster_id):
    v1 = client.CoreV1Api(api_client=K8SApiClient(project_id, zone, cluster_id))
    return v1.delete_namespace(name=namespace, body=client.V1DeleteOptions())


def remove_clusterrole_binding(project_id, cluster_id, zone, shipper_name, crb_name):
    # remove clusterrolebind
    rbac_k8s_client = client.RbacAuthorizationV1Api(api_client=K8SApiClient(project_id, zone, cluster_id))
    res = rbac_k8s_client.list_cluster_role_binding()
    try:
        for item in res.items:
            if item.metadata.name == crb_name:
                rbac_k8s_client.delete_cluster_role_binding(crb_name)
                print(f"\nsuccessfully deleted: {crb_name}")
                break
    except NotFound as e:
        print(f"could not find {crb_name}:\n{e}")
    except Exception as e:
        print(f"\n{shipper_name} cluster role binding deletion has failed, please manually delete {crb_name}:\n{e}")
        print(f"kubectl delete clusterrolebinding {crb_name}")


def list_namespace_deployments(project_id, cluster_id, zone, namespace, keywords=None):
    apis_api = client.AppsV1Api(api_client=K8SApiClient(project_id, zone, cluster_id))
    resp = apis_api.list_namespaced_deployment(namespace=namespace)
    if keywords:
        deps = [dep.metadata.name for dep in resp.items if any(ele in dep.metadata.name for ele in keywords)]
    else:
        deps = [dep.metadata.name for dep in resp.items]
    return deps


def remove_deployments_in_namespace(project_id, cluster_id, zone, namespace, deps=None, keywords=None):
    """
    remove deployments in namespace
    :param project_id:
    :param cluster_id:
    :param zone:
    :param namespace: string, namespace name
    :param deps: list, a list of all deployments to be deleted under the given namespace
    :param keywords: list, in case deps wasn't supplied, all deployments who's in their name one of the items in keyword
                        will be listed for deletion
    :return:
    """
    if not deps:
        deps = list_namespace_deployments(project_id, cluster_id, zone, namespace, keywords)
    print(f"deployments for deletion:\n{deps}")
    k8s_beta = client.AppsV1Api(api_client=K8SApiClient(project_id, zone, cluster_id))
    for dep in deps:
        print(f"removing {dep} deployment")
        resp = k8s_beta.delete_namespaced_deployment(name=dep,
                                                     namespace=namespace,
                                                     body=client.V1DeleteOptions(propagation_policy='Foreground',
                                                                                 grace_period_seconds=5))


def remove_client_deployments(project_id, cluster_id, zone, namespace):
    print("remove_client_deployments")
    remove_deployments_in_namespace(project_id, cluster_id, zone, namespace, None, ["client", "bootstrap"])
