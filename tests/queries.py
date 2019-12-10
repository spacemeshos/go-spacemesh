import re
import time
import collections
from datetime import datetime
from elasticsearch_dsl import Search, Q

from tests.context import ES


TS_FORMAT = "%Y-%m-%dT%H:%M:%S.%fZ"

dt = datetime.now()
todaydate = dt.strftime("%Y.%m.%d")
current_index = 'kubernetes_cluster-' + todaydate


# for convenience
def get_pod_name_and_namespace_queries(pod_name, namespace):
    return Q("match_phrase", kubernetes__pod_name=pod_name) & \
           Q("match_phrase", kubernetes__namespace_name=namespace)


def set_time_frame_query(from_ts=None, to_ts=None):
    if from_ts and to_ts:
        res_q = Q({'bool': {'range': {'@timestamp': {'gte': from_ts, 'lte': to_ts}}}})
    elif from_ts and not to_ts:
        res_q = Q({'bool': {'range': {'@timestamp': {'gte': from_ts}}}})
    elif to_ts:
        res_q = Q({'bool': {'range': {'@timestamp': {'lte': to_ts}}}})
    else:
        print("could not set time frame, both time limits are None")
        res_q = None

    return res_q


# ================================== MESSSAGE CONTENT ==================================

def get_block_creation_msgs(namespace, pod_name, find_fails=False, from_ts=None, to_ts=None):
    created_block_msg = "I've created a block in layer"
    return get_all_msg_containing(namespace, pod_name, created_block_msg, find_fails, from_ts, to_ts)


def get_done_syncing_msgs(namespace, pod_name):
    done_waiting_msg = "Done waiting for ticks and validation"
    return get_all_msg_containing(namespace, pod_name, done_waiting_msg)


def get_app_started_msgs(namespace, pod_name):
    app_started_msg = "App started"
    return get_all_msg_containing(namespace, pod_name, app_started_msg)


def get_all_msg_containing(namespace, pod_name, msg_data, find_fails=False, from_ts=None, to_ts=None):
    """
    Queries for all logs with msg_data in their content {"M": msg_data}
    also, it's optional to add timestamps as time frame, if only one is passed
    then messages will hit from from_ts on or from to_ts back

    :param namespace: string, session id
    :param pod_name: string, filter for pod name entry
    :param msg_data: string, message content
    :param find_fails: boolean, whether to print unmatched pods (query_message)
    :param from_ts: string, find results from this time stamp on (%Y-%m-%dT%H:%M:%S.%fZ)
    :param to_ts: string, find results before this time stamp (%Y-%m-%dT%H:%M:%S.%fZ)

    :return: list, all matching hits
    """

    queries = []
    if from_ts or to_ts:
        queries = [set_time_frame_query(from_ts, to_ts)]

    msg = {"M": msg_data}
    hit_lst = query_message(current_index, namespace, pod_name, msg, find_fails, queries=queries)
    print(f"found {str(len(hit_lst))} messages containing (match_phrase): {msg_data}\n")
    return hit_lst


def get_blocks_msgs_of_pod(namespace, pod_name):
    return get_blocks_and_layers(namespace, pod_name)


def get_blocks_per_node_and_layer(deployment):
    return get_blocks_and_layers(deployment, deployment)


def get_blocks_and_layers(namespace, pod_name, find_fails=False):
    # I've created a block in layer %v. id: %v, num of transactions: %v, votes: %d,
    # viewEdges: %d, atx %v, atxs:%v
    blocks = get_all_msg_containing(namespace, pod_name, "I've created a block in layer", find_fails)
    nodes = sort_by_nodeid(blocks)
    layers = sort_by_layer(blocks)

    return nodes, layers


def get_layers(namespace, find_fails=True):
    layers = get_all_msg_containing(namespace, namespace, "release tick", find_fails)
    ids = [int(x.layer_id) for x in layers]
    return ids


# ============================== END MESSSAGE CONTENT ==================================

def get_podlist(namespace, depname):
    api = ES().get_search_api()
    fltr = get_pod_name_and_namespace_queries(depname, namespace)
    s = Search(index=current_index, using=api).query('bool').filter(fltr)
    hits = list(s.scan())
    podnames = set([hit.kubernetes.pod_name for hit in hits])
    return podnames


def get_pod_logs(namespace, pod_name):
    api = ES().get_search_api()
    fltr = get_pod_name_and_namespace_queries(pod_name, namespace)
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


def poll_query_message(indx, namespace, client_po_name, fields, findFails=False, startTime=None, expected=None, query_time_out=120):

    hits = query_message(indx, namespace, client_po_name, fields, findFails, startTime)
    if expected is None:
        return hits

    time_passed = 0
    while len(hits) < expected:
        if time_passed > query_time_out:
            print("Timeout expired when polling on query expected={0}, hits={1}".format(expected, len(hits)))
            break

        time.sleep(10)
        time_passed += 10
        hits = query_message(indx, namespace, client_po_name, fields, findFails, startTime)
    return hits


def query_message(indx, namespace, client_po_name, fields, find_fails=False, start_time=None, queries=None):
    # TODO : break this to smaller functions ?
    es = ES().get_search_api()
    fltr = get_pod_name_and_namespace_queries(client_po_name, namespace)
    for key in fields:
        fltr = fltr & Q("match_phrase", **{key: fields[key]})

    # append extra queries
    if queries:
        for q in queries:
            fltr = fltr & q

    s = Search(index=indx, using=es).query('bool', filter=[fltr])
    hits = list(s.scan())

    print("====================================================================")
    print("Report for `{0}` in deployment -  {1}  ".format(fields, client_po_name))
    print("Number of hits: ", len(hits))
    print("====================================================================")
    print("Benchmark results:")
    if len(hits) > 0:
        ts = [hit["T"] for hit in hits]

        first = start_time if start_time is not None else datetime.strptime(
            min(ts).replace("T", " ", ).replace("Z", ""), "%Y-%m-%d %H:%M:%S.%f")
        last = datetime.strptime(max(ts).replace("T", " ", ).replace("Z", ""), "%Y-%m-%d %H:%M:%S.%f")

        delta = last - first
        print("First: {0}, Last: {1}, Delta: {2}".format(first, last, delta))
        # TODO: compare to previous runs.
        print("====================================================================")
    else:
        print("no hits")
    if find_fails:
        print("Looking for pods that didn't hit:")
        podnames = set([hit.kubernetes.pod_name for hit in hits])
        newfltr = get_pod_name_and_namespace_queries(client_po_name, namespace)

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


atx = collections.namedtuple('atx', ['atx_id', 'layer_id', 'published_in_epoch'])


# TODO this can be a util function
def parseAtx(log_messages):
    node2blocks = {}
    for x in log_messages:
        nid = re.split(r'\.', x.N)[0]
        matx = atx(x.atx_id, x.layer_id, x.epoch_id)
        if nid in node2blocks:
            node2blocks[nid].append(matx)
        else:
            node2blocks[nid] = [matx]
    return node2blocks


# sets log messages into a dictionary where keys=node_id and
# value is a dictionary of blocks and layers
# TODO this can be a util function
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


# sets log messages into a dictionary where keys=layer_id and
# value is a dictionary of blocks and layers
# TODO this can be a util function
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


# TODO this can be a util function
def print_node_stats(nodes):
    for node in nodes:
        print("node " + node + " blocks created: " + str(len(nodes[node]["blocks"])))
        for layer in nodes[node]["layers"]:
            print("blocks created in layer " + str(layer) + " : " + str(len(nodes[node]["layers"][layer])))


# TODO this can be a util function
def print_layer_stat(layers):
    for l in layers:
        print("blocks created in layer " + str(l) + " : " + str(len(layers[l])))


# TODO this can be a util function
def get_latest_layer(deployment):
    layers = get_layers(deployment)
    layers.sort(reverse=True)
    if len(layers) == 0:
        return 0
    return layers[0]


def wait_for_latest_layer(deployment, min_layer_id, layers_per_epoch):
    while True:
        lyr = get_latest_layer(deployment)
        print("current layer " + str(lyr))
        if lyr >= min_layer_id and lyr % layers_per_epoch == 0:
            return lyr
        time.sleep(10)


def get_atx_per_node(deployment):
    # based on log: atx published! id: %v, prevATXID: %v, posATXID: %v, layer: %v,
    # published in epoch: %v, active set: %v miner: %v view %v
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
    fltr = get_pod_name_and_namespace_queries(client_po_name, namespace)
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
    fltr = get_pod_name_and_namespace_queries(client_po_name, namespace)
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

# =====================================================================================
# Hare queries
# =====================================================================================


def query_hare_output_set(indx, ns, layer):
    hits = query_message(indx, ns, ns, {'M': 'Consensus process terminated', 'layer_id': str(layer)}, True)
    lst = [h.current_set for h in hits]

    return lst


def query_round_1(indx, ns, layer):
    return query_message(indx, ns, ns, {'M': 'status round ended', 'is_svp_ready': 'true', 'layer_id': str(layer)}, False)


def query_round_2(indx, ns, layer):
    hits = query_message(indx, ns, ns, {'M': 'proposal round ended', 'layer_id': str(layer)}, False)
    filtered = list(filter(lambda x: x.proposed_set != "nil", hits))

    return filtered


def query_round_3(indx, ns, layer):
    return query_message(indx, ns, ns, {'M': 'message sent', 'msg_type': 'Commit', 'layer_id': str(layer)}, False)


def query_pre_round(indx, ns, layer):
    return query_message(indx, ns, ns, {'M': 'Fatal: PreRound ended with empty set', 'layer_id': str(layer)}, False)


def query_no_svp(indx, ns):
    return query_message(indx, ns, ns, {'M': 'status round ended', 'is_svp_ready': 'false'}, False)


def query_empty_set(indx, ns):
    return query_message(indx, ns, ns, {'M': 'Fatal: PreRound ended with empty set'}, False)


def query_new_iteration(indx, ns):
    return query_message(indx, ns, ns, {'M': 'Starting new iteration'}, False)


def query_mem_usage(indx, ns):
    return query_message(indx, ns, ns, {'M': 'json_mem_data'}, False)


def query_atx_published(indx, ns, layer):
    return query_message(indx, ns, ns, {'M': 'atx published', 'layer_id': str(layer)}, False)
