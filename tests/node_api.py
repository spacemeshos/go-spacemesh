import multiprocessing as mp

from tests.utils import api_call_async


def shutdown_miners(namespace, ips):
    api_endpoint = "v1/node/shutdown"
    processes = []
    manager = mp.Manager()
    return_list = manager.list()
    for ip in ips:
        data = {}
        args = (ip, data, api_endpoint, namespace, return_list)
        processes.append(mp.Process(target=api_call_async, args=args))
    for p in processes:
        p.start()
    for p in processes:
        p.join()
    return return_list
