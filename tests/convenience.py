from datetime import datetime


TIMESTAMP_FMT = "%Y-%m-%dT%H:%M:%S.%fZ"

PRINT_SEP = "===================================================================="


def print_nl_end(*args):
    print(*args, "\n")


def print_nl_start(*args):
    print("\n", *args)


def convert_ts_to_datetime(ts):
    return datetime.strptime(ts, TIMESTAMP_FMT)
