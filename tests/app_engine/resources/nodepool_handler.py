from google.auth import compute_engine
from google.api_core.exceptions import NotFound
from google.cloud.container_v1 import ClusterManagerClient
import time


def remove_node_pool(project_name, pool_name, cluster_name, zone, retry=800, interval=10):
    credentials = compute_engine.Credentials()
    cluster_manager_client = ClusterManagerClient(credentials=credentials)
    success = False
    while retry and not success:
        try:
            res = cluster_manager_client.delete_node_pool(project_id=project_name, zone=zone,
                                                          cluster_id=cluster_name, node_pool_id=pool_name)
            print(f"res:\n{res}")
            success = True
        except NotFound as e:
            raise Exception(f"{pool_name} node pool does not exist:\n{e}")
        except Exception as e:
            retry -= 1
            if retry > 0:
                print(f"sleeping for {interval} seconds before trying again")
                time.sleep(interval)
