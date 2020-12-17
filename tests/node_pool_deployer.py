from functools import wraps
import re
from time import time

import tests.utils as ut


def timing(func):
    @wraps(func)
    def wrapper(*args, **kwargs):
        start = time()
        result = func(*args, **kwargs)
        end = time()
        return result, end-start
    return wrapper


class NodePoolDep:
    resources = ["bootstrap", "client"]
    gcloud_delete = "yes | gcloud container node-pools delete {pool_name} --cluster={cluster_name} --zone={zone}"
    gcloud_cmd = 'yes | gcloud beta container --project "{project_name}" node-pools create "{pool_name}" ' \
                 '--cluster "{cluster_name}" --zone "{zone}" --node-version "{node_version}" ' \
                 '--machine-type "custom-{cpu}-{mem}" --image-type "COS" --disk-type "{disk_type}" ' \
                 '--disk-size "{disk_size}" --node-labels {labels} --metadata disable-legacy-endpoints=true ' \
                 '--node-taints {taints} --scopes "https://www.googleapis.com/auth/devstorage.read_only",' \
                 '"https://www.googleapis.com/auth/logging.write",' \
                 '"https://www.googleapis.com/auth/monitoring",' \
                 '"https://www.googleapis.com/auth/servicecontrol",' \
                 '"https://www.googleapis.com/auth/service.management.readonly",' \
                 '"https://www.googleapis.com/auth/trace.append" ' \
                 '--num-nodes "{num_nodes}" --enable-autoscaling --min-nodes "{min_nodes}" --max-nodes "{max_nodes}" ' \
                 '--no-enable-autoupgrade --enable-autorepair --max-surge-upgrade 1 --max-unavailable-upgrade 0'

    def __init__(self, testconfig):
        self.testconfig = testconfig
        self.namespace = testconfig["namespace"]
        self.pool_name = f"pool-{self.namespace}"
        self.cluster_name = "spacemesh-cluster-elk"
        self.zone = "us-west1-a"
        self.default_config = {
            "project_name": "spacemesh-198810",
            "pool_name": self.pool_name,
            "cluster_name": self.cluster_name,
            "zone": self.zone,
            "node_version": "1.16.15-gke.4300",
            "cpu": "18",
            "mem": 22*1024,
            "disk_type": "pd-standard",
            "disk_size": 100,
            "labels": f"namespace={self.namespace}",
            "taints": f'namespace={self.namespace}:NoSchedule',
            "num_nodes": 0,
            "min_nodes": 0,
            "max_nodes": 10,
        }

    @timing
    def add_node_pool(self):
        print("adding node pool:", self.pool_name)
        # in advance we also add 4 CPUs and another 16GB of memory for precaution
        cpu_per_node, mem_per_node = self.calculate_cpu_and_mem_per_node()

        config = {
            "num_nodes": 2,
            "max_nodes": 2,
            "cpu": cpu_per_node,
            "mem": mem_per_node,
        }
        # get formatted cmd
        config = self.merge_conf(config)
        cmd = self.gcloud_cmd.format(**config)
        ret_code = ut.exec_wait(cmd, retry=60, interval=10)

    def calculate_cpu_and_mem_per_node(self):
        from math import ceil
        max_cpus_per_node = 96
        total_cpu, _ = self.get_total_cpu_and_mem()
        total_cpu += 4
        dividor = 6
        for i in range(1, 6):
            if total_cpu / i > max_cpus_per_node:
                continue
            dividor = i
            break

        cpu_per_node = ceil(total_cpu / dividor)
        if cpu_per_node % 2:
            cpu_per_node += 1

        mem_per_node = (cpu_per_node + 5) * 1024
        return cpu_per_node, mem_per_node

    def remove_node_pool(self):
        print("removing node pool")
        ut.exec_wait(self.gcloud_delete.format(pool_name=self.pool_name, cluster_name=self.cluster_name, zone=self.zone)
                     , retry=60, interval=10)

    def get_total_cpu_and_mem(self):
        _, cpu, mem = self.get_spec_resources("bootstrap")
        total_pods = int(self.testconfig["total_pods"])
        total_cpu = total_pods * cpu
        total_mem = total_pods * mem
        # if "clientv2" in self.testconfig.keys() and "clientv2" not in self.resources:
        #     self.resources.append("clientv2")
        #
        # total_cpu = 0
        # total_mem = 0
        # for res in self.resources:
        #     replicas, cpu, mem = self.get_spec_resources(res)
        #     total_cpu += replicas * cpu
        #     total_mem += replicas * mem
        # # TODO: maybe move validation somewhere else?
        if total_cpu % 2:
            print("adding additional CPU, total CPU was not divide by 2, total CPUs:", total_cpu)
            total_cpu += 1
        if total_mem % 512:
            raise Exception("total memory is not a 1024 multiplication")

        return total_cpu, total_mem

    def get_spec_resources(self, res_name):
        if res_name not in self.testconfig.keys():
            raise Exception(f"resource {res_name} was not found in configurations")

        replicas = self.testconfig[res_name]["replicas"]
        cpu = int(self.testconfig[res_name]["resources"]["limits"]["cpu"])
        mem_str = self.testconfig[res_name]["resources"]["limits"]["memory"]
        match = re.match("^\d+", mem_str)
        if not match:
            raise Exception("could not extract memory quantity")
        mem_int = int(match.group(0))
        return replicas, cpu, mem_int

    def merge_conf(self, config_to_merge):
        config = self.default_config.copy()
        config.update(config_to_merge)
        return config
