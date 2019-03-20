

class NodeInfo():

    def __init__(self):
        self.bs_pod_name = ''
        self.bs_pod_ip='0.0.0.0'
        self.bs_deployment_name = ''
        self.bs_key = ''


class ContainerSpec():

    REPLACEABLE_ARGS = ['randcon', 'oracle_server', 'bootnodes', 'genesis_time']

    def __init__(self, cname, cimage, centry, **kwargs):
        self.name = cname
        self.image = cimage
        self.entrypoint = centry
        self.args = kwargs

    def update_deployment(self, dep):
        containers = dep['spec']['template']['spec']['containers']
        for c in containers:
            if c['name'] == self.name:
                #update the container specs
                if self.image:
                    c['image'] = self.image
                if self.entrypoint:
                    c['command'] = self.entrypoint
                c['args'] = self.update_args(c['args'], **(self.args))
                break
        return dep

    def update_args(self, args_input_yaml, **kwargs):
        for k in kwargs:
            if k not in ContainerSpec.REPLACEABLE_ARGS:
                raise Exception("Unknown arg: {0} - parsing error")
            replaced = False
            for i, arg in enumerate(args_input_yaml):
                if arg[2:].replace('-', '_') == k:
                    # replace the value
                    args_input_yaml[i + 1] = kwargs[k]
                    replaced = True
                    break
            if not replaced:
                args_input_yaml += (['--{0}'.format(k.replace('_', '-')), kwargs[k]])
        return args_input_yaml
