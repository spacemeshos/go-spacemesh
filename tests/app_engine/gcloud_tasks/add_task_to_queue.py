import datetime
from google.cloud import tasks_v2
import json


def create_google_cloud_task(queue_params, payload, path='/', in_seconds=None, **kwargs):
    """

    :param queue_params: dictionary, a dictionary with keys project_id, queue_name, queue_zone for resolving
                         gcloud queue path.
    :param payload: dictionary, the arguments for the app engine task.
    :param path: string, path to task relative url
    :param in_seconds: int, number of seconds to delay task.
    :param kwargs: dictionary, optional - additional arguments.
    :return:
    """
    # Create a client.
    client = tasks_v2.CloudTasksClient()
    # these params will be validated in case 'is_dump' var is True
    # these params are vital for resolving queue path
    gcloud_params_val = ["project_id", "queue_name", "queue_zone"]
    for g_param in gcloud_params_val:
        if g_param not in queue_params:
            raise ValueError(f"missing {g_param} param in order to resolve queue")
    # add optional arguments to payload (currently necessary only for dumping)
    payload["dump_params"] = kwargs
    # Construct the fully qualified queue name.
    parent = client.queue_path(queue_params["project_id"], queue_params["queue_zone"], queue_params["queue_name"])
    # Construct the request body.
    task = {
        'app_engine_http_request': {  # Specify the type of request.
            'http_method': tasks_v2.HttpMethod.POST,
            'relative_uri': path
        }
    }
    if payload is not None:
        if isinstance(payload, dict):
            # Convert dict to JSON string
            payload = json.dumps(payload)
            # specify http content-type to application/json
            task["app_engine_http_request"]["headers"] = {"Content-type": "application/json"}
        # The API expects a payload of type bytes.
        converted_payload = payload.encode()
        # Add the payload to the request.
        task['app_engine_http_request']['body'] = converted_payload
    if in_seconds is not None:
        # Convert "seconds from now" into an rfc3339 datetime string.
        timestamp = datetime.datetime.utcnow() + datetime.timedelta(seconds=in_seconds)
        # Add the timestamp to the tasks.
        task['schedule_time'] = timestamp
    # Use the client to build and send the task.
    response = client.create_task(parent=parent, task=task)
    print('\nCreated task {}'.format(response.name))
    return response
