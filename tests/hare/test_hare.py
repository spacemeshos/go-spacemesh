import time
from elasticsearch_dsl import Search, Q
from pytest_testconfig import config as testconfig

from tests.fixtures import set_namespace, load_config, init_session, set_docker_images, session_id
from tests.test_bs import setup_clients, save_log_on_exit, setup_oracle, setup_bootstrap, create_configmap
from tests.test_bs import get_elastic_search_api
from tests.test_bs import current_index, wait_genesis


class Set:
    def __init__(self, values):
        self.values = {}
        for v in values:
            self.values[v] = v

    @classmethod
    def from_str(cls, s):
        values = [x.strip() for x in s.split(',')]
        return cls(values)

    def contains(self, val):
        return val in self.values

    def equals(self, other):
        for v in self.values:
            if v not in other.values:
                return False
        return True


def consistency(outputs):
    for s in outputs:
        for g in outputs:
            if not g.equals(s):
                return False
    return True


def v1(outputs, intersection):
    for v in intersection:
        if not outputs.contains(v):
            return False
    return True


def validate(outputs):
    sets = [Set.from_str(o) for o in outputs]

    if not consistency(sets):
        print("consistency failed")
        return False
    return True


def query_hare_output_set(indx, namespace, client_po_name):
    es = get_elastic_search_api()
    fltr = Q("match_phrase", kubernetes__namespace_name=namespace) & \
           Q("match_phrase", kubernetes__pod_name=client_po_name) & \
           Q("match_phrase", M="Consensus process terminated")
    s = Search(index=indx, using=es).query('bool', filter=[fltr])
    hits = list(s.scan())

    lst = []
    for h in hits:
        lst.append(h.set_values)
    return lst


# ==============================================================================
#    TESTS
# ==============================================================================


NUM_OF_EXPECTED_ROUNDS = 5
EFK_LOG_PROPAGATION_DELAY = 10


def test_hare_sanity(setup_clients, wait_genesis, save_log_on_exit):

    # Need to wait for 1 full iteration + the time it takes the logs to propagate to ES
    delay = int(testconfig['client']['args']['hare-round-duration-sec']) * NUM_OF_EXPECTED_ROUNDS + \
            EFK_LOG_PROPAGATION_DELAY + int(testconfig['client']['args']['hare-wakeup-delta'])
    print("Going to sleep for {0}".format(delay))
    time.sleep(delay)
    lst = query_hare_output_set(current_index, testconfig['namespace'], setup_clients.deployment_id)
    total = testconfig['bootstrap']['replicas'] + testconfig['client']['replicas']
    assert total == len(lst)
    assert validate(lst)
