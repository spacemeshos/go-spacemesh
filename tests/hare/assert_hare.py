from pytest_testconfig import config as testconfig

from tests.delayed_assert.delayed_assert import expect, assert_expectations
from tests.queries import query_hare_output_set, query_round_1, query_round_2, query_round_3, query_pre_round, \
    query_no_svp, query_empty_set, query_new_iteration, query_mem_usage


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


def assert_all(curr_idx, ns, layer):
    total = testconfig['bootstrap']['replicas'] + testconfig['client']['replicas']

    # assert no node ended up with an empty set at the end of the pre-round
    lst = query_pre_round(curr_idx, ns, layer)
    assert 0 == len(lst)

    # assert all nodes had SVP ready at the end of round 1
    lst = query_round_1(curr_idx, ns, layer)
    assert total == len(lst)

    # assert all nodes had a (non-nil) proposal at the end of round 2
    lst = query_round_2(curr_idx, ns, layer)
    assert total == len(lst)

    # assert at least f+1 has committed at the end of round 3
    lst = query_round_3(curr_idx, ns, layer)
    f = int(testconfig['client']['args']['hare-max-adversaries'])
    assert len(lst) >= f + 1

    # assert termination output set
    lst = query_hare_output_set(curr_idx, ns, layer)
    assert total == len(lst)
    assert validate(lst)


def total_eligibilities(hits):
    return sum(hit.eligibility_count for hit in hits)


def expect_consensus_process(curr_idx, ns, layer, total, f):
    msg = 'layer=%s expected=%s actual=%s'
    # assert no node ended up with an empty set at the end of the pre-round
    lst = query_pre_round(curr_idx, ns, layer)
    expect(0 == len(lst), msg % (layer, 0, len(lst)))

    # assert all nodes had SVP ready at the end of round 1
    lst = query_round_1(curr_idx, ns, layer)
    expect(total == len(lst), msg % (layer, total, len(lst)))

    # assert all nodes had a (non-nil) proposal at the end of round 2
    lst = query_round_2(curr_idx, ns, layer)
    expect(total == len(lst), msg % (layer, total, len(lst)))

    # assert at least f+1 has committed at the end of round 3
    lst = query_round_3(curr_idx, ns, layer)
    expect(total_eligibilities(lst) >= f + 1,
           'layer=%s total_eligibilities=%d f=%d' % (layer, total_eligibilities(lst), f))

    # assert termination output set
    lst = query_hare_output_set(curr_idx, ns, layer)
    expect(total == len(lst), msg % (layer, total, len(lst)))
    expect(validate(lst), msg % (layer, 'true', 'false'))


def expect_hare(curr_idx, ns, min_l, max_l, total, f):
    layer = min_l
    while layer <= max_l:
        expect_consensus_process(curr_idx, ns, layer, total, f)
        layer = layer + 1

    assert_expectations()


def validate_hare(indx, ns):
    lst = query_empty_set(indx, ns)
    expect(0 == len(lst), 'query no empty set')

    lst = query_no_svp(indx, ns)
    expect(0 == len(lst), 'query no svp')

    lst = query_new_iteration(indx, ns)
    expect(0 == len(lst), 'query no new iteration')

    assert_expectations()


def get_max_mem_usage(i, n):
    x = query_mem_usage(i, n)
    max_mem = 0
    for y in x:
        try:
            z = int(y.Alloc_max)
            if z > max_mem:
                max_mem = z
        except:
            continue

    return max_mem
