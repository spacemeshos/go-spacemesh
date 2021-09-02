import base64
from google.cloud import storage
from io import BytesIO
import os

from tests.k8s_handler import list_all_pods_names_in_namespace, get_stream_from_pod
from tests.config import get_state_bucket

SM_NODE_DATA_PATH = "root/spacemesh/1"
# this is the default data dir path unless mentioned differently using "poetdir" flag
POET_DATA_PATH = "root/.poet"


def upload_data_to_google_storage(bucket_name, dir_name, file_name, data):
    print(f"uploading data to {os.path.join(bucket_name, dir_name, file_name)}")
    storage_client = storage.Client()
    # Creating bucket object
    bucket = storage_client.get_bucket(bucket_name)
    file_path = os.path.join(dir_name, file_name) if dir_name else file_name
    # Name of the object to be stored in the bucket
    blob = bucket.blob(file_path)
    blob.upload_from_file(BytesIO(data))
    print(f"upload ended successfully for {os.path.join(bucket_name, dir_name, file_name)}")


def upload_from_pod_to_gcloud_storage(namespace, pod_name, data_path=None, bucket_name=None,
                                      bucket_dir=None, bucket_file=None, container=None):
    """
    upload data directories from a pod.
    the data is being compressed to a tar.gz format and returned in base64 following a bug in k8s stream -
    fails to pass binary data

    :param namespace: string, namespace name
    :param pod_name: string, pod name
    :param data_path: string, data directory path in pod
    :param bucket_name: string, the name of the bucket to dump data to
    :param bucket_dir: string, the name of the directory (inside the given bucket) to dump data to
    :param bucket_file: string, tar file name to contain data, default is {pod_name}.tar.gz
    :param container: string, the name of the container inside the pod
    :return:
    """
    # save data directory to stdout using tar command
    # z - compress the data, c - create new
    # we pipe to base64 because kubernetes.stream is failing to pass bytes
    exec_command = ['/bin/sh', '-c', f'tar zc {data_path} | base64']
    # open a stream to the pod
    try:
        resp = get_stream_from_pod(pod_name, namespace, exec_command, container=container)
    except Exception as e:
        print(f"quitting following an exception while streaming:\n{e}")
        exit(1)
    # if file_name is not supplied set file name to pod_name with tar.gz ending
    file_name = f"{pod_name}.tar.gz" if not bucket_file else bucket_file
    # if pod contains more than one container each container data will be saved under a directory
    # with that container name
    if bucket_dir and container:
        bucket_dir = os.path.join(bucket_dir, container)
    elif container:
        bucket_dir = container
    upload_data_to_google_storage(bucket_name, bucket_dir, file_name, base64.b64decode(resp))


def upload_sm_from_pod_to_gcloud_storage(namespace, pod_name, data_path=SM_NODE_DATA_PATH, bucket_name=None,
                                         bucket_dir=None, bucket_file=None, container=None):
    """
    upload the data from data_path to google cloud storage bucket

    :param namespace: string, namespace name
    :param pod_name: string, the pod name, the one that contains the data
    :param data_path: string, path to data inside the pod
    :param bucket_name: string, the bucket name on google storage
    :param bucket_dir: string, the name of the directory to create inside the bucket
    :param bucket_file: string, file name to create inside the directory
    :param container: string, the container name inside the pod (needed only if there's more than 1 container)
    :return:
    """
    # set bucket name to default value if not supplied
    bucket_name = get_state_bucket() if not bucket_name else bucket_name
    # if dir_name is not supplied set default value to namespace
    bucket_dir = namespace if not bucket_dir else bucket_dir
    upload_from_pod_to_gcloud_storage(namespace, pod_name, data_path, bucket_name, bucket_dir, bucket_file, container)


def upload_whole_network(namespace, bucket_dir_name=None):
    print(f"uploading client poet and bootstrap data from namespace {namespace}")
    pods_names = list_all_pods_names_in_namespace(namespace)
    for pod in pods_names:
        if 'client' in pod:
            upload_sm_from_pod_to_gcloud_storage(namespace, pod, bucket_dir=bucket_dir_name)
        if 'poet' in pod:
            upload_sm_from_pod_to_gcloud_storage(namespace, pod, bucket_dir=bucket_dir_name, data_path=POET_DATA_PATH,
                                                 container="poet")
        if 'bootstrap' in pod:
            upload_sm_from_pod_to_gcloud_storage(namespace, pod, bucket_dir=bucket_dir_name, data_path=SM_NODE_DATA_PATH,
                                                 container="bootstrap")


def list_files_in_path(bucket_name, path=None):
    """Lists all the blobs in the bucket."""
    storage_client = storage.Client()
    blobs = storage_client.list_blobs(bucket_name, prefix=path)
    return [blob.name for blob in blobs]
