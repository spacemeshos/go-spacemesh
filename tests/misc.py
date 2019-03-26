import string
import random


class NodeInfo():

    def __init__(self, dep_id=None):
        self.pod_name = ''
        self.pod_ip= '0.0.0.0'
        self.deployment_name = ''
        self.key = ''
        self.deployment_id = dep_id if dep_id else  NodeInfo.random_deployment_id()

    @staticmethod
    def random_deployment_id():
        # Just alphanumeric characters
        chars = string.ascii_lowercase + string.digits
        return ''.join((random.choice(chars)) for x in range(4))


class ContainerSpec():

    REPLACEABLE_ARGS = ['randcon', 'oracle_server', 'bootnodes', 'genesis_time']

    def __init__(self, cname, cimage, centry):
        self.name = cname
        self.image = cimage
        self.entrypoint = centry
        self.args = {}

    def append_args(self, **kwargs):
        self.args.update(kwargs)

    def update_deployment(self, dep):
        containers = dep['spec']['template']['spec']['containers']
        for c in containers:
            if c['name'] == self.name:
                #update the container specs
                if self.image:
                    c['image'] = self.image
                if self.entrypoint:
                    c['command'] = self.entrypoint
                c['args'] = self._update_args(c['args'], **(self.args))
                break
        return dep

    def _update_args(self, args_input_yaml, **kwargs):
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
