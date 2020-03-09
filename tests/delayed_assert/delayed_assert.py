'''
Implements one form of delayed assertions.

Interface is 2 functions:

  expect(expr, msg=None)
  : Evaluate 'expr' as a boolean, and keeps track of failures

  assert_expectations()
  : raises an assert if an expect() calls failed

Usage Example:

    from expectations import expect, assert_expectations

    def test_should_pass():
        expect(1 == 1, 'one is one')
        assert_expectations()

    def test_should_fail():
        expect(1 == 2, 'one is two')
        expect(1 == 3, 'one is three')
        assert_expectations()
'''


# ---------------------------------------------------

def expect(expr, msg=None):
    'keeps track of failed expectations'
    if not expr:
        _log_failure(msg)


def assert_expectations():
    'raise an assert if there are any failed expectations'
    if _failed_expectations:
        assert False, _report_failures()


# ---------------------------------------------------

import inspect
import os.path

_failed_expectations = []


def _log_failure(msg=None):
    (filename, line, funcname, contextlist) = inspect.stack()[2][1:5]
    filename = os.path.basename(filename)
    _failed_expectations.append('file "%s", line %s, in %s()%s\n' %
                                (filename, line, funcname, (('\n%s' % msg) if msg else '')))


def _report_failures():
    global _failed_expectations
    if _failed_expectations:
        (filename, line, funcname) = inspect.stack()[2][1:4]
        report = [
            '\n\nassert_expectations() called from',
            '"%s" line %s, in %s()\n' % (os.path.basename(filename), line, funcname),
            'Failed Expectations:%s\n' % len(_failed_expectations)]
        for i, failure in enumerate(_failed_expectations, start=1):
            report.append('%d: %s' % (i, failure))
        _failed_expectations = []
    return '\n'.join(report)

# ---------------------------------------------------
# _log_failure() notes
#
# stack() returns a list of frame records
#   0 is the _log_failure() function
#   1 is the expect() function
#   2 is the function that called expect(), that's what we want
#
# a frame record is a tuple like this:
#   (frame, filename, line, funcname, contextlist, index)
# we're mainly interested in the middle 4,
# ---------------------------------------------------
