from kubernetes import client
from kubernetes.client import Configuration, ApiClient


class K8SApiClient(ApiClient):

    def __init__(self):
        client_config = Configuration()
        client_config.connection_pool_maxsize = 32
        super(K8SApiClient, self).__init__(configuration=client_config)


class CoreV1ApiClient(client.CoreV1Api):

    def __init__(self):
        super(CoreV1ApiClient, self).__init__(api_client=K8SApiClient())


class ContainerSpec:
    """
    This class holds all specifications regarding the running suite.
    Specifications may be gathered from k8s spec files (at ./k8s), suite file
    and manual changes made on the go
    """
    REPLACEABLE_ARGS = ['randcon', 'oracle_server', 'bootnodes', 'genesis_time', 'poet_server']

    def __init__(self, cname, specs):
        self.name = cname
        self.image = specs['image']
        self.entrypoint = [specs['command']]
        self.resources = None if 'resources' not in specs else specs['resources']
        self.args = {}

    def append_args(self, **kwargs):
        self.args.update(kwargs)

    def update_deployment(self, dep):
        if dep['kind'] == 'Pod':
            containers = dep['spec']['containers']
        else:
            containers = dep['spec']['template']['spec']['containers']
        for c in containers:
            if c['name'] == self.name:
                # update the container specs
                if self.image:
                    c['image'] = self.image
                if self.entrypoint:
                    c['command'] = self.entrypoint
                if self.resources:
                    c['resources'] = self.resources
                c['args'] = self._update_args(c['args'], **(self.args))
                break
        return dep

    @staticmethod
    def _update_args(args_input_yaml, **kwargs):
        for k in kwargs:
            replaced = False
            if k in ContainerSpec.REPLACEABLE_ARGS:
                for i, arg in enumerate(args_input_yaml):
                    if arg[2:].replace('-', '_') == k:
                        # replace the value
                        args_input_yaml[i + 1] = kwargs[k]
                        replaced = True
                        break
            if not replaced:
                args_input_yaml += (['--{0}'.format(k.replace('_', '-')), kwargs[k]])
        return args_input_yaml
