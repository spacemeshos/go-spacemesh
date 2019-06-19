from pytest_testconfig import config as testconfig
from tests.queries import query_hare_output_set, query_round_1, query_round_2, query_round_3, query_pre_round, query_message
from tests.delayed_assert.delayed_assert import expect, assert_expectations


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

    # assert no node ended up with an empty set at the end of the pre-round
    lst = query_pre_round(curr_idx, ns, 1)
    assert 0 == len(lst)

    # assert all nodes had SVP ready at the end of round 1
    lst = query_round_1(curr_idx, ns, 1)
    assert total == len(lst)

    # assert all nodes had a (non-nil) proposal at the end of round 2
    lst = query_round_2(curr_idx, ns, 1)
    assert total == len(lst)

    # assert at least f+1 has committed at the end of round 3
    lst = query_round_3(curr_idx, ns, 1)
    f = int(testconfig['client']['args']['hare-max-adversaries'])
    assert len(lst) >= f + 1

    # assert termination output set
    lst = query_hare_output_set(curr_idx, ns, 1)
    assert total == len(lst)
    assert validate(lst)


def expect_consensus_process(curr_idx, ns, layer):
    total = testconfig['bootstrap']['replicas'] + testconfig['client']['replicas']
    msg = 'layer=%s' % layer
    # assert no node ended up with an empty set at the end of the pre-round
    lst = query_pre_round(curr_idx, ns, layer)
    expect(0 == len(lst), msg)

    # assert all nodes had SVP ready at the end of round 1
    lst = query_round_1(curr_idx, ns, layer)
    expect(total == len(lst), msg)

    # assert all nodes had a (non-nil) proposal at the end of round 2
    lst = query_round_2(curr_idx, ns, layer)
    expect(total == len(lst), msg)

    # assert at least f+1 has committed at the end of round 3
    lst = query_round_3(curr_idx, ns, layer)
    f = int(testconfig['client']['args']['hare-max-adversaries'])
    expect(len(lst) >= f + 1, msg)

    # assert termination output set
    lst = query_hare_output_set(curr_idx, ns, layer)
    expect(total == len(lst), msg)
    expect(validate(lst), msg)


def expect_hare(curr_idx, ns, min_l, max_l):
    layer = min_l
    while layer <= max_l:
        expect_consensus_process(curr_idx, ns, layer)
        layer = layer + 1

    assert_expectations()

