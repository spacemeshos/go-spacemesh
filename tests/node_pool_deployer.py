from math import ceil
import re

import tests.utils as ut


class NodePoolDep:
    resources = ["bootstrap", "client"]
    gcloud_delete = 'gcloud --quiet container --project="{project_name}" node-pools delete {pool_name} ' \
                    '--cluster={cluster_name} --zone={zone}'
    gcloud_cmd = 'gcloud --quiet beta container --project "{project_name}" node-pools create "{pool_name}" ' \
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
        self.cluster_name = ut.get_env("CLUSTER_NAME")
        self.zone = ut.get_env("CLUSTER_ZONE")
        self.project_name = ut.get_env("PROJECT_NAME")
        self.default_config = {
            "project_name": self.project_name,
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

    @ut.timing
    def add_node_pool(self):
        print("adding node pool:", self.pool_name)
        retry = 800
        interval = 10
        # in advance we also add 4 CPUs and another 16GB of memory for precaution
        num_nodes, cpu_per_node, mem_per_node = self.calculate_cpu_and_mem_per_node()
        config = {
            "num_nodes": num_nodes,
            "max_nodes": num_nodes,
            "cpu": cpu_per_node,
            "mem": mem_per_node,
        }
        # get formatted cmd
        config = self.merge_conf(config)
        cmd = self.gcloud_cmd.format(**config)
        ut.exec_wait(cmd, retry=retry, interval=interval)

    def calculate_cpu_and_mem_per_node(self):
        """
        adding extra_cpus and extra 5GB of memory on each node for precaution.
        :return: dividor: the number of nodes to be started,
                 cpu_per_node: how many CPUs per node, must divide by 2 according to GCP specifications limits
                 mem per node: how much memory per node, must be withing GCP specifications limits
        """
        # GCP limit
        max_cpus_per_node = 96
        # extra cpus for k8s processes
        extra_cpus_per_node = 2
        total_cpu, _ = self.get_total_cpu_and_mem()
        dividor = 6
        for i in range(1, 6):
            if ceil(total_cpu / i) + extra_cpus_per_node > max_cpus_per_node:
                continue
            dividor = i
            break

        cpu_per_node = ceil(total_cpu / dividor) + extra_cpus_per_node
        if cpu_per_node % 2:
            cpu_per_node += 1
        # memory is equal to the number of CPUs in GB with adding additional 5GB of memory
        mem_per_node = (cpu_per_node + 5) * 1024
        return dividor, cpu_per_node, mem_per_node

    @ut.timing
    def remove_node_pool(self):
        print("\nremoving node pool")
        retry = 800
        interval = 10
        ut.exec_wait(
            self.gcloud_delete.format(project_name=self.project_name, pool_name=self.pool_name,
                                      cluster_name=self.cluster_name, zone=self.zone),
            retry=retry, interval=interval
        )

    def get_total_cpu_and_mem(self):
        _, cpu, mem = self.get_spec_resources("bootstrap")
        total_pods = int(self.testconfig["total_pods"])
        total_cpu = total_pods * cpu
        total_mem = total_pods * mem
        if total_cpu % 2:
            print("adding additional CPU, total CPU count not divisible by 2, total CPUs:", total_cpu)
            total_cpu += 1
        if total_mem % 512:
            raise Exception("total memory is not divisible by 512")

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
