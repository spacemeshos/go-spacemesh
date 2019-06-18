import os
import re
import time
from datetime import datetime

from elasticsearch import Elasticsearch
from elasticsearch_dsl import Search, Q

dt = datetime.now()
todaydate = dt.strftime("%Y.%m.%d")
current_index = 'kubernetes_cluster-' + todaydate


def singleton(cls):
    instance = [None]

    def wrapper(*args, **kwargs):
        if instance[0] is None:
            instance[0] = cls(*args, **kwargs)
        return instance[0]

    return wrapper


@singleton
class ES:

    def __init__(self):
        ES_PASSWD = os.getenv("ES_PASSWD")
        if not ES_PASSWD:
            raise Exception("Unknown Elasticsearch password. Please check 'ES_PASSWD' environment variable")
        self.es = Elasticsearch("http://elastic.spacemesh.io",
                                http_auth=("spacemesh", ES_PASSWD), port=80, timeout=90)

    def get_search_api(self):
        return self.es


def get_podlist(namespace, depname):
    api = ES().get_search_api()
    fltr = Q("match_phrase", kubernetes__pod_name=depname) & Q("match_phrase", kubernetes__namespace_name=namespace)
    s = Search(index=current_index, using=api).query('bool').filter(fltr)
    hits = list(s.scan())
    podnames = set([hit.kubernetes.pod_name for hit in hits])
    return podnames


def get_pod_logs(namespace, pod_name):
    api = ES().get_search_api()
    fltr = Q("match_phrase", kubernetes__pod_name=pod_name) & Q("match_phrase", kubernetes__namespace_name=namespace)
    s = Search(index=current_index, using=api).query('bool').filter(fltr).sort("time")
    res = s.execute()
    full = Search(index=current_index, using=api).query('bool').filter(fltr).sort("time").extra(size=res.hits.total)
    res = full.execute()
    hits = list(res.hits)
    print("Writing ${0} log lines for pod {1} ".format(len(hits), pod_name))
    with open('./logs/' + pod_name + '.txt', 'w') as f:
        for i in hits:
            f.write(i.log)


def get_podlist_logs(namespace, podlist):
    for i in podlist:
        get_pod_logs(namespace, i)


def get_deployment_logs(namespace, depname):
    lst = get_podlist(namespace, depname)
    print("Getting pod list ", lst)
    get_podlist_logs(namespace, lst)


def query_message(indx, namespace, client_po_name, fields, findFails=False, startTime=None):
    # TODO : break this to smaller functions ?
    es = ES().get_search_api()
    fltr = Q("match_phrase", kubernetes__namespace_name=namespace) & \
           Q("match_phrase", kubernetes__pod_name=client_po_name)
    for f in fields:
        fltr = fltr & Q("match_phrase", **{f: fields[f]})
    s = Search(index=indx, using=es).query('bool', filter=[fltr])
    hits = list(s.scan())

    print("====================================================================")
    print("Report for `{0}` in deployment -  {1}  ".format(fields, client_po_name))
    print("Number of hits: ", len(hits))
    print("====================================================================")
    print("Benchmark results:")
    if len(hits) > 0:
        ts = [hit["T"] for hit in hits]

        first = startTime if startTime is not None else datetime.strptime(min(ts).replace("T", " ", ).replace("Z", ""),
                                                                          "%Y-%m-%d %H:%M:%S.%f")
        last = datetime.strptime(max(ts).replace("T", " ", ).replace("Z", ""), "%Y-%m-%d %H:%M:%S.%f")

        delta = last - first
        print("First: {0}, Last: {1}, Delta: {2}".format(first, last, delta))
        # TODO: compare to previous runs.
        print("====================================================================")
    else:
        print("no hits")
    if findFails:
        print("Looking for pods that didn't hit:")
        podnames = [hit.kubernetes.pod_name for hit in hits]
        newfltr = Q("match_phrase", kubernetes__namespace_name=namespace) & \
                  Q("match_phrase", kubernetes__pod_name=client_po_name)

        for p in podnames:
            newfltr = newfltr & ~Q("match_phrase", kubernetes__pod_name=p)
        s2 = Search(index=current_index, using=es).query('bool', filter=[newfltr])
        hits2 = list(s2.scan())
        unsecpods = set([hit.kubernetes.pod_name for hit in hits2])
        if len(unsecpods) == 0:
            print("None. yay!")
        else:
            print(unsecpods)
        print("====================================================================")

    s = list(hits)
    return s


def parseAtx(log_messages):
    node2blocks = {}
    for x in log_messages:
        nid = re.split(r'\.', x.N)[0]
        m = re.findall(r'(?<=\b:\s)(\w+)|(?<=view\s)(\w+)', x.M)
        if nid in node2blocks:
            node2blocks[nid].append(m)
        else:
            node2blocks[nid] = [m]
    return node2blocks


def sort_by_nodeid(log_messages):
    node2blocks = {}
    for x in log_messages:
        id = re.split(r'\.', x.N)[0]
        m = re.findall(r'\d+', x.M)
        layer = m[0]
        # blocks - list of all blocks, layers - map of blocks per layer
        if id in node2blocks:
            node2blocks[id]["blocks"].append(m)
            if layer in node2blocks[id]["layers"]:
                node2blocks[id]["layers"][layer].append(m)
            else:
                node2blocks[id]["layers"][layer] = [m]
        else:
            node2blocks[id] = {"blocks": [m], "layers": {m[0]: [m]}}
    return node2blocks


def sort_by_layer(log_messages):
    blocks_per_layer = {}
    for x in log_messages:
        m = re.findall(r'\d+', x.M)
        layer = m[0]
        if layer in blocks_per_layer:
            blocks_per_layer[layer].append(m)
        else:
            blocks_per_layer[layer] = [m]
    return blocks_per_layer


def print_node_stats(nodes):
    for node in nodes:
        print("node " + node + " blocks created: " + str(len(nodes[node]["blocks"])))
        for layer in nodes[node]["layers"]:
            print("blocks created in layer " + str(layer) + " : " + str(len(nodes[node]["layers"][layer])))


def print_layer_stat(layers):
    for l in layers:
        print("blocks created in layer " + str(l) + " : " + str(len(layers[l])))


def get_blocks_per_node_and_layer(deployment):
    # I've created a block in layer %v. id: %v, num of transactions: %v, votes: %d, viewEdges: %d, atx %v, atxs:%v
    block_fields = {"M": "I've created a block in layer"}
    blocks = query_message(current_index, deployment, deployment, block_fields, True)
    print("found " + str(len(blocks)) + " blocks")
    nodes = sort_by_nodeid(blocks)
    layers = sort_by_layer(blocks)

    return nodes, layers


def get_layers(deployment):
    block_fields = {"M": "release tick"}
    layers = query_message(current_index, deployment, deployment, block_fields, True)
    ids = [int(re.findall(r'\d+', x.M)[0]) for x in layers]
    return ids


def get_latest_layer(deployment):
    layers = get_layers(deployment)
    layers.sort(reverse=True)
    return layers[0]


def wait_for_latest_layer(deployment, layer_id):
    while True:
        lyr = get_latest_layer(deployment)
        if lyr >= layer_id:
            return

        print("current layer " + str(lyr))
        time.sleep(10)


def get_atx_per_node(deployment):
    # based on log: atx published! id: %v, prevATXID: %v, posATXID: %v, layer: %v, published in epoch: %v, active set: %v miner: %v view %v
    block_fields = {"M": "atx published"}
    atx_logs = query_message(current_index, deployment, deployment, block_fields, True)
    print("found " + str(len(atx_logs)) + " atxs")
    nodes = parseAtx(atx_logs)
    return nodes


def get_nodes_up(deployment):
    # based on log:
    block_fields = {"M": "Starting Spacemesh"}
    logs = query_message(current_index, deployment, deployment, block_fields, True)
    print("found " + str(len(logs)) + " nodes up")
    return len(logs)


def find_dups(indx, namespace, client_po_name, fields, max=1):
    """
    finds elasticsearch hits that are duplicates per kubernetes_pod_name.
    The max field represents the number of times the message
    should show up if the indexing was functioning well.

    Usage : find_dups(current_index, "t7t9e", "client-t7t9e-28qj7",
    {'M':'new_gossip_message', 'protocol': 'api_test_gossip'}, 10)
    """

    es = ES().get_search_api()
    fltr = Q("match_phrase", kubernetes__namespace_name=namespace) & \
           Q("match_phrase", kubernetes__pod_name=client_po_name)
    for f in fields:
        fltr = fltr & Q("match_phrase", **{f: fields[f]})
    s = Search(index=indx, using=es).query('bool', filter=[fltr])
    hits = list(s.scan())

    dups = []
    counting = {}

    for hit in hits:
        counting[hit.kubernetes.pod_name] = 1 if hit.kubernetes.pod_name not in counting else counting[
                                                                                                  hit.kubernetes.pod_name] + 1
        if counting[hit.kubernetes.pod_name] > max and hit.kubernetes.pod_name not in counting:
            dups.append(hit.kubernetes.pod_name)

    print("Total hits: {0}".format(len(hits)))
    print("Duplicate count {0}".format(len(dups)))
    print(dups)


def find_missing(indx, namespace, client_po_name, fields, min=1):
    # Usage : find_dups(current_index, "t7t9e", "client-t7t9e-28qj7",
    # {'M':'new_gossip_message', 'protocol': 'api_test_gossip'}, 10)

    es = ES().get_search_api()
    fltr = Q("match_phrase", kubernetes__namespace_name=namespace) & \
           Q("match_phrase", kubernetes__pod_name=client_po_name)
    for f in fields:
        fltr = fltr & Q("match_phrase", **{f: fields[f]})
    s = Search(index=indx, using=es).query('bool', filter=[fltr])
    hits = list(s.scan())

    miss = []
    counting = {}

    for hit in hits:
        counting[hit.kubernetes.pod_name] = 1 if hit.kubernetes.pod_name not in counting else counting[
                                                                                                  hit.kubernetes.pod_name] + 1

    for pod in counting:
        if counting[pod] < min:
            miss.append(pod)

    print("Total hits: {0}".format(len(hits)))
    print("Missing count {0}".format(len(miss)))
    print(miss)


def query_hare_output_set(indx, ns):
    hits = query_message(indx, ns, ns, {'M': 'Consensus process terminated'}, True)
    lst = [h.set_values for h in hits]

    return lst


def query_round_1(indx, ns):
    return query_message(indx, ns, ns, {'M': 'Round 1 ended', 'is_svp_ready': 'true'}, True)


def query_round_2(indx, ns):
    hits = query_message(indx, ns, ns, {'M': 'Round 2 ended'}, True)
    filtered = list(filter(lambda x: x.proposed_set != "nil", hits))

    return filtered


def query_round_3(indx, ns):
    return query_message(indx, ns, ns, {'M': 'Round 3 ended: committing'}, True)


def query_pre_round(indx, ns):
    return query_message(indx, ns, ns, {'M': 'Fatal: PreRound ended with empty set'}, True)
