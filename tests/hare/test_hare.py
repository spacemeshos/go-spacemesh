import time

from pytest_testconfig import config as testconfig

from tests.queries import query_hare_output_set, query_round_1, query_round_2, query_round_3, query_pre_round, query_message
from tests.test_bs import current_index, setup_clients, setup_oracle, setup_poet, setup_bootstrap, create_configmap, \
    wait_genesis, save_log_on_exit
from tests.fixtures import init_session, load_config, set_namespace, session_id, set_docker_images


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


def assert_all(curr_idx, ns):
    total = testconfig['bootstrap']['replicas'] + testconfig['client']['replicas']

    # assert_result
    lst = query_hare_output_set(curr_idx, ns)
    assert total == len(lst)
    assert validate(lst)

    # assert round 1
    lst = query_round_1(curr_idx, ns)
    assert total == len(lst)

    # assert round 2
    lst = query_round_2(curr_idx, ns)
    assert total == len(lst)

    # assert round 3
    lst = query_round_3(curr_idx, ns)
    f = int(testconfig['client']['args']['hare-max-adversaries'])
    assert len(lst) >= f + 1

    # assert pre round
    lst = query_pre_round(curr_idx, ns)
    assert 0 == len(lst)


# ==============================================================================
#    TESTS
# ==============================================================================


NUM_OF_EXPECTED_ROUNDS = 5
EFK_LOG_PROPAGATION_DELAY = 10


def test_hare_sanity(setup_bootstrap, setup_clients, wait_genesis):
    # Need to wait for 1 full iteration + the time it takes the logs to propagate to ES
    delay = int(testconfig['client']['args']['hare-round-duration-sec']) * NUM_OF_EXPECTED_ROUNDS + \
            EFK_LOG_PROPAGATION_DELAY + int(testconfig['client']['args']['hare-wakeup-delta'])
    print("Going to sleep for {0}".format(delay))
    time.sleep(delay)

    assert_all(current_index, testconfig['namespace'])
