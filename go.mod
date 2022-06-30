module github.com/devspace-cloud/devspace-cloud-plugin

require (
	github.com/blang/semver v3.5.1+incompatible
	github.com/devspace-cloud/devspace v1.1.1-0.20200724074930-ec77a1851818
	github.com/juju/testing v0.0.0-20220203020004-a0ff61f03494 // indirect
	github.com/machinebox/graphql v0.2.2
	github.com/matryer/is v1.4.0 // indirect
	github.com/mattn/go-colorable v0.1.7 // indirect
	github.com/mgutz/ansi v0.0.0-20170206155736-9520e82c474b
	github.com/mitchellh/go-homedir v1.1.0
	github.com/otiai10/mint v1.3.3 // indirect
	github.com/pkg/errors v0.9.1
	github.com/rhysd/go-github-selfupdate v0.0.0-20180520142321-41c1bbb0804a
	github.com/rjeczalik/notify v0.9.2 // indirect
	github.com/shirou/gopsutil v3.21.11+incompatible // indirect
	github.com/sirupsen/logrus v1.4.2
	github.com/spf13/cobra v1.0.0
	github.com/spf13/pflag v1.0.5
	github.com/tklauser/go-sysconf v0.3.10 // indirect
	github.com/yusufpapurcu/wmi v1.2.2 // indirect
	golang.org/x/sys v0.0.0-20220627191245-f75cf1eec38b // indirect
	gopkg.in/yaml.v2 v2.4.0
	gotest.tools v2.2.0+incompatible
	k8s.io/api v0.18.2
	k8s.io/apimachinery v0.18.6
	k8s.io/client-go v0.18.2
)

replace (
	github.com/Azure/go-autorest => github.com/Azure/go-autorest v13.3.0+incompatible
	github.com/agl/ed25519 => github.com/agl/ed25519 v0.0.0-20170116200512-5312a6153412
)

go 1.13
