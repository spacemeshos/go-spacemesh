from datetime import datetime, timedelta

from tests.misc import ContainerSpec
from elasticsearch import Elasticsearch
from tests.fixtures import load_config, bootstrap_deployment_info, client_deployment_info, set_namespace
from tests.test_bs import setup_clients, save_log_on_exit, setup_oracle, setup_bootstrap, create_configmap
import os
from os import path
import pytest
import pytz
import re
import subprocess
import time
import yaml
from kubernetes import client
from kubernetes.client.rest import ApiException
from pytest_testconfig import config as testconfig
from tests.test_bs import get_elastic_search_api
from tests.test_bs import current_index, wait_genesis
from elasticsearch_dsl import Search, Q
from dotenv import load_dotenv

# ==============================================================================
#    TESTS
# ==============================================================================


def test_sync_sanity(set_namespace, setup_clients, setup_bootstrap, save_log_on_exit):

    # load_dotenv()
    # wait_genesis()

    print("done ")