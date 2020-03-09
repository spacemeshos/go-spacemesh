import argparse
from datetime import datetime
import time

TIMESTAMP_FMT = "%Y-%m-%dT%H:%M:%S.%fZ"

PRINT_SEP = "===================================================================="

LOG_ENTRIES = {"message": "M"}


def print_nl_end(*args):
    print(*args, "\n")


def print_nl_start(*args):
    print("\n", *args)


def convert_ts_to_datetime(ts):
    return datetime.strptime(ts, TIMESTAMP_FMT)


def sleep_print_backwards(tts):
    print(f"\nsleeping for {tts} seconds\n")
    while tts != 0:
        tts -= 1
        print(f" {tts} seconds left     ", end="\r")
        time.sleep(1)


def str2bool(v):
    if isinstance(v, bool):
        return v
    if v.lower() in ('yes', 'true', 't', 'y', '1'):
        return True
    elif v.lower() in ('no', 'false', 'f', 'n', '0'):
        return False
    else:
        raise argparse.ArgumentTypeError('Boolean value expected.')


def print_hits_entry_count(hits, log_entry):
    """
    prints number of seen values

    :param hits: list, all query results
    :param log_entry: string, what part of the log do we want to sum
    """

    if log_entry not in LOG_ENTRIES.keys():
        raise ValueError(f"unknown log entry {log_entry}")

    result = {}
    entry = LOG_ENTRIES[log_entry]

    for hit in hits:
        entry_val = getattr(hit, entry)
        if entry_val not in result:
            result[entry_val] = 1
            continue

        result[entry_val] += 1

    for key in result:
        print(f"found {result[key]} appearances of '{key}' in hits")
