import os
from datetime import datetime

from elasticsearch import Elasticsearch
from elasticsearch_dsl import Search, Q

dt = datetime.now()
todaydate = dt.strftime("%Y.%m.%d")
current_index = 'kubernetes_cluster-'+todaydate

def get_elastic_search_api():
    # TODO: convert to a singleton, preferably from main test runner. (maybe just get the api as param for queries?)
    ES_PASSWD = os.getenv("ES_PASSWD")
    if not ES_PASSWD:
        raise Exception("Unknown Elasticsearch password. Please check 'ES_PASSWD' environment variable")
    es = Elasticsearch("http://es.spacemesh.io",
                       http_auth=("spacemesh", ES_PASSWD), port=80)
    return es


def get_podlist(namespace, depname):
    api = get_elastic_search_api()
    fltr = Q("match_phrase", kubernetes__pod_name=depname) & Q("match_phrase", kubernetes__namespace_name=namespace)
    s = Search(index=current_index, using=api).query('bool').filter(fltr)
    hits = list(s.scan())
    podnames = set([hit.kubernetes.pod_name for hit in hits])
    return podnames


def get_pod_logs(namespace, pod_name):
    api = get_elastic_search_api()
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
