unless Vagrant.has_plugin?("vagrant-vbguest")
  raise 'Please install vagrant-vbguest by running: vagrant plugin install vagrant-vbguest'
end
Vagrant.configure("2") do |config|
  config.vm.box = "centos/7"
  config.vm.hostname = "spacemesh"
  config.vm.network :private_network, ip: "192.168.0.100"
  config.vm.synced_folder ".", "/home/vagrant/spacemesh/src/github.com/spacemeshos/go-spacemesh"
  config.vm.provision "shell", path: "vagrant/provision.sh"
end
