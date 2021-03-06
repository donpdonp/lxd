package main

import (
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"io/ioutil"
	"os"
	"sort"
	"strings"
	"syscall"

	"github.com/olekukonko/tablewriter"
	"gopkg.in/yaml.v2"

	"github.com/lxc/lxd"
	"github.com/lxc/lxd/shared"
	"github.com/lxc/lxd/shared/api"
	"github.com/lxc/lxd/shared/gnuflag"
	"github.com/lxc/lxd/shared/i18n"
	"github.com/lxc/lxd/shared/termios"
)

type configCmd struct {
	expanded bool
}

func (c *configCmd) showByDefault() bool {
	return true
}

func (c *configCmd) flags() {
	gnuflag.BoolVar(&c.expanded, "expanded", false, i18n.G("Show the expanded configuration"))
}

func (c *configCmd) configEditHelp() string {
	return i18n.G(
		`### This is a yaml representation of the configuration.
### Any line starting with a '# will be ignored.
###
### A sample configuration looks like:
### name: container1
### profiles:
### - default
### config:
###   volatile.eth0.hwaddr: 00:16:3e:e9:f8:7f
### devices:
###   homedir:
###     path: /extra
###     source: /home/user
###     type: disk
### ephemeral: false
###
### Note that the name is shown but cannot be changed`)
}

func (c *configCmd) usage() string {
	return i18n.G(
		`Usage: lxc config <subcommand> [options]

Change container or server configuration options.

*Container configuration*

lxc config get [<remote>:][container] <key>
    Get container or server configuration key.

lxc config set [<remote>:][container] <key> <value>
    Set container or server configuration key.

lxc config unset [<remote>:][container] <key>
    Unset container or server configuration key.

lxc config show [<remote>:][container] [--expanded]
    Show container or server configuration.

lxc config edit [<remote>:][container]
    Edit configuration, either by launching external editor or reading STDIN.

*Device management*

lxc config device add [<remote>:]<container> <device> <type> [key=value...]
    Add a device to a container.

lxc config device get [<remote>:]<container> <device> <key>
    Get a device property.

lxc config device set [<remote>:]<container> <device> <key> <value>
    Set a device property.

lxc config device unset [<remote>:]<container> <device> <key>
    Unset a device property.

lxc config device list [<remote>:]<container>
    List devices for container.

lxc config device show [<remote>:]<container>
    Show full device details for container.

lxc config device remove [<remote>:]<container> <name>
    Remove device from container.

*Client trust store management*

lxc config trust list [<remote>:]
    List all trusted certs.

lxc config trust add [<remote>:] <certfile.crt>
    Add certfile.crt to trusted hosts.

lxc config trust remove [<remote>:] [hostname|fingerprint]
    Remove the cert from trusted hosts.

*Examples*

cat config.yaml | lxc config edit <container>
    Update the container configuration from config.yaml.

lxc config device add [<remote>:]container1 <device-name> disk source=/share/c1 path=opt
    Will mount the host's /share/c1 onto /opt in the container.

lxc config set [<remote>:]<container> limits.cpu 2
    Will set a CPU limit of "2" for the container.

lxc config set core.https_address [::]:8443
    Will have LXD listen on IPv4 and IPv6 port 8443.

lxc config set core.trust_password blah
    Will set the server's trust password to blah.`)
}

func (c *configCmd) doSet(config *lxd.Config, args []string, unset bool) error {
	if len(args) != 4 {
		return errArgs
	}

	// [[lxc config]] set dakara:c1 limits.memory 200000
	remote, container := config.ParseRemoteAndContainer(args[1])
	d, err := lxd.NewClient(config, remote)
	if err != nil {
		return err
	}

	key := args[2]
	value := args[3]

	if !termios.IsTerminal(int(syscall.Stdin)) && value == "-" {
		buf, err := ioutil.ReadAll(os.Stdin)
		if err != nil {
			return fmt.Errorf(i18n.G("Can't read from stdin: %s"), err)
		}
		value = string(buf[:])
	}

	if unset {
		st, err := d.ContainerInfo(container)
		if err != nil {
			return err
		}

		_, ok := st.Config[key]
		if !ok {
			return fmt.Errorf(i18n.G("Can't unset key '%s', it's not currently set."), key)
		}
	}

	return d.SetContainerConfig(container, key, value)
}

func (c *configCmd) run(config *lxd.Config, args []string) error {
	if len(args) < 1 {
		return errUsage
	}

	switch args[0] {

	case "unset":
		if len(args) < 2 {
			return errArgs
		}

		// Deal with local server
		if len(args) == 2 {
			c, err := lxd.NewClient(config, config.DefaultRemote)
			if err != nil {
				return err
			}

			ss, err := c.ServerStatus()
			if err != nil {
				return err
			}

			_, ok := ss.Config[args[1]]
			if !ok {
				return fmt.Errorf(i18n.G("Can't unset key '%s', it's not currently set."), args[1])
			}

			_, err = c.SetServerConfig(args[1], "")
			return err
		}

		// Deal with remote server
		remote, container := config.ParseRemoteAndContainer(args[1])
		if container == "" {
			c, err := lxd.NewClient(config, remote)
			if err != nil {
				return err
			}

			ss, err := c.ServerStatus()
			if err != nil {
				return err
			}

			_, ok := ss.Config[args[1]]
			if !ok {
				return fmt.Errorf(i18n.G("Can't unset key '%s', it's not currently set."), args[1])
			}

			_, err = c.SetServerConfig(args[2], "")
			return err
		}

		// Deal with container
		args = append(args, "")
		return c.doSet(config, args, true)

	case "set":
		if len(args) < 3 {
			return errArgs
		}

		// Deal with local server
		if len(args) == 3 {
			c, err := lxd.NewClient(config, config.DefaultRemote)
			if err != nil {
				return err
			}

			_, err = c.SetServerConfig(args[1], args[2])
			return err
		}

		// Deal with remote server
		remote, container := config.ParseRemoteAndContainer(args[1])
		if container == "" {
			c, err := lxd.NewClient(config, remote)
			if err != nil {
				return err
			}

			_, err = c.SetServerConfig(args[2], args[3])
			return err
		}

		// Deal with container
		return c.doSet(config, args, false)

	case "trust":
		if len(args) < 2 {
			return errArgs
		}

		switch args[1] {
		case "list":
			var remote string
			if len(args) == 3 {
				remote = config.ParseRemote(args[2])
			} else {
				remote = config.DefaultRemote
			}

			d, err := lxd.NewClient(config, remote)
			if err != nil {
				return err
			}

			trust, err := d.CertificateList()
			if err != nil {
				return err
			}

			data := [][]string{}
			for _, cert := range trust {
				fp := cert.Fingerprint[0:12]

				certBlock, _ := pem.Decode([]byte(cert.Certificate))
				if certBlock == nil {
					return fmt.Errorf(i18n.G("Invalid certificate"))
				}

				cert, err := x509.ParseCertificate(certBlock.Bytes)
				if err != nil {
					return err
				}

				const layout = "Jan 2, 2006 at 3:04pm (MST)"
				issue := cert.NotBefore.Format(layout)
				expiry := cert.NotAfter.Format(layout)
				data = append(data, []string{fp, cert.Subject.CommonName, issue, expiry})
			}

			table := tablewriter.NewWriter(os.Stdout)
			table.SetAutoWrapText(false)
			table.SetAlignment(tablewriter.ALIGN_LEFT)
			table.SetRowLine(true)
			table.SetHeader([]string{
				i18n.G("FINGERPRINT"),
				i18n.G("COMMON NAME"),
				i18n.G("ISSUE DATE"),
				i18n.G("EXPIRY DATE")})
			sort.Sort(StringList(data))
			table.AppendBulk(data)
			table.Render()

			return nil
		case "add":
			var remote string
			if len(args) < 3 {
				return fmt.Errorf(i18n.G("No certificate provided to add"))
			} else if len(args) == 4 {
				remote = config.ParseRemote(args[2])
			} else {
				remote = config.DefaultRemote
			}

			d, err := lxd.NewClient(config, remote)
			if err != nil {
				return err
			}

			fname := args[len(args)-1]
			cert, err := shared.ReadCert(fname)
			if err != nil {
				return err
			}

			name, _ := shared.SplitExt(fname)
			return d.CertificateAdd(cert, name)
		case "remove":
			var remote string
			if len(args) < 3 {
				return fmt.Errorf(i18n.G("No fingerprint specified."))
			} else if len(args) == 4 {
				remote = config.ParseRemote(args[2])
			} else {
				remote = config.DefaultRemote
			}

			d, err := lxd.NewClient(config, remote)
			if err != nil {
				return err
			}

			return d.CertificateRemove(args[len(args)-1])
		default:
			return errArgs
		}

	case "show":
		remote := config.DefaultRemote
		container := ""
		if len(args) > 1 {
			remote, container = config.ParseRemoteAndContainer(args[1])
		}

		d, err := lxd.NewClient(config, remote)
		if err != nil {
			return err
		}

		var data []byte

		if len(args) == 1 || container == "" {
			config, err := d.ServerStatus()
			if err != nil {
				return err
			}

			brief := config.Writable()
			data, err = yaml.Marshal(&brief)
			if err != nil {
				return err
			}
		} else {
			var brief api.ContainerPut
			if shared.IsSnapshot(container) {
				config, err := d.SnapshotInfo(container)
				if err != nil {
					return err
				}

				brief = api.ContainerPut{
					Profiles:  config.Profiles,
					Config:    config.Config,
					Devices:   config.Devices,
					Ephemeral: config.Ephemeral,
				}
				if c.expanded {
					brief = api.ContainerPut{
						Profiles:  config.Profiles,
						Config:    config.ExpandedConfig,
						Devices:   config.ExpandedDevices,
						Ephemeral: config.Ephemeral,
					}
				}
			} else {
				config, err := d.ContainerInfo(container)
				if err != nil {
					return err
				}

				brief = config.Writable()
				if c.expanded {
					brief.Config = config.ExpandedConfig
					brief.Devices = config.ExpandedDevices
				}
			}

			data, err = yaml.Marshal(&brief)
			if err != nil {
				return err
			}
		}

		fmt.Printf("%s", data)

		return nil

	case "get":
		if len(args) > 3 || len(args) < 2 {
			return errArgs
		}

		remote := config.DefaultRemote
		container := ""
		key := args[1]
		if len(args) > 2 {
			remote, container = config.ParseRemoteAndContainer(args[1])
			key = args[2]
		}

		d, err := lxd.NewClient(config, remote)
		if err != nil {
			return err
		}

		if container != "" {
			resp, err := d.ContainerInfo(container)
			if err != nil {
				return err
			}
			fmt.Println(resp.Config[key])
		} else {
			resp, err := d.ServerStatus()
			if err != nil {
				return err
			}

			value := resp.Config[key]
			if value == nil {
				value = ""
			} else if value == true {
				value = "true"
			} else if value == false {
				value = "false"
			}

			fmt.Println(value)
		}
		return nil

	case "profile":
	case "device":
		if len(args) < 2 {
			return errArgs
		}
		switch args[1] {
		case "list":
			return c.deviceList(config, "container", args)
		case "add":
			return c.deviceAdd(config, "container", args)
		case "remove":
			return c.deviceRm(config, "container", args)
		case "get":
			return c.deviceGet(config, "container", args)
		case "set":
			return c.deviceSet(config, "container", args)
		case "unset":
			return c.deviceUnset(config, "container", args)
		case "show":
			return c.deviceShow(config, "container", args)
		default:
			return errArgs
		}

	case "edit":
		if len(args) < 1 {
			return errArgs
		}

		remote := config.DefaultRemote
		container := ""
		if len(args) > 1 {
			remote, container = config.ParseRemoteAndContainer(args[1])
		}

		d, err := lxd.NewClient(config, remote)
		if err != nil {
			return err
		}

		if len(args) == 1 || container == "" {
			return c.doDaemonConfigEdit(d)
		}

		return c.doContainerConfigEdit(d, container)

	default:
		return errArgs
	}

	return errArgs
}

func (c *configCmd) doContainerConfigEdit(client *lxd.Client, cont string) error {
	// If stdin isn't a terminal, read text from it
	if !termios.IsTerminal(int(syscall.Stdin)) {
		contents, err := ioutil.ReadAll(os.Stdin)
		if err != nil {
			return err
		}

		newdata := api.ContainerPut{}
		err = yaml.Unmarshal(contents, &newdata)
		if err != nil {
			return err
		}
		return client.UpdateContainerConfig(cont, newdata)
	}

	// Extract the current value
	config, err := client.ContainerInfo(cont)
	if err != nil {
		return err
	}

	brief := config.Writable()
	data, err := yaml.Marshal(&brief)
	if err != nil {
		return err
	}

	// Spawn the editor
	content, err := shared.TextEditor("", []byte(c.configEditHelp()+"\n\n"+string(data)))
	if err != nil {
		return err
	}

	for {
		// Parse the text received from the editor
		newdata := api.ContainerPut{}
		err = yaml.Unmarshal(content, &newdata)
		if err == nil {
			err = client.UpdateContainerConfig(cont, newdata)
		}

		// Respawn the editor
		if err != nil {
			fmt.Fprintf(os.Stderr, i18n.G("Config parsing error: %s")+"\n", err)
			fmt.Println(i18n.G("Press enter to start the editor again"))

			_, err := os.Stdin.Read(make([]byte, 1))
			if err != nil {
				return err
			}

			content, err = shared.TextEditor("", content)
			if err != nil {
				return err
			}
			continue
		}
		break
	}
	return nil
}

func (c *configCmd) doDaemonConfigEdit(client *lxd.Client) error {
	// If stdin isn't a terminal, read text from it
	if !termios.IsTerminal(int(syscall.Stdin)) {
		contents, err := ioutil.ReadAll(os.Stdin)
		if err != nil {
			return err
		}

		newdata := api.ServerPut{}
		err = yaml.Unmarshal(contents, &newdata)
		if err != nil {
			return err
		}

		_, err = client.UpdateServerConfig(newdata)
		return err
	}

	// Extract the current value
	config, err := client.ServerStatus()
	if err != nil {
		return err
	}

	brief := config.Writable()
	data, err := yaml.Marshal(&brief)
	if err != nil {
		return err
	}

	// Spawn the editor
	content, err := shared.TextEditor("", []byte(c.configEditHelp()+"\n\n"+string(data)))
	if err != nil {
		return err
	}

	for {
		// Parse the text received from the editor
		newdata := api.ServerPut{}
		err = yaml.Unmarshal(content, &newdata)
		if err == nil {
			_, err = client.UpdateServerConfig(newdata)
		}

		// Respawn the editor
		if err != nil {
			fmt.Fprintf(os.Stderr, i18n.G("Config parsing error: %s")+"\n", err)
			fmt.Println(i18n.G("Press enter to start the editor again"))

			_, err := os.Stdin.Read(make([]byte, 1))
			if err != nil {
				return err
			}

			content, err = shared.TextEditor("", content)
			if err != nil {
				return err
			}
			continue
		}
		break
	}
	return nil
}

func (c *configCmd) deviceAdd(config *lxd.Config, which string, args []string) error {
	if len(args) < 5 {
		return errArgs
	}
	remote, name := config.ParseRemoteAndContainer(args[2])

	client, err := lxd.NewClient(config, remote)
	if err != nil {
		return err
	}

	devname := args[3]
	devtype := args[4]
	var props []string
	if len(args) > 5 {
		props = args[5:]
	} else {
		props = []string{}
	}

	var resp *api.Response
	if which == "profile" {
		resp, err = client.ProfileDeviceAdd(name, devname, devtype, props)
	} else {
		resp, err = client.ContainerDeviceAdd(name, devname, devtype, props)
	}
	if err != nil {
		return err
	}
	if which != "profile" {
		err = client.WaitForSuccess(resp.Operation)
	}
	if err == nil {
		fmt.Printf(i18n.G("Device %s added to %s")+"\n", devname, name)
	}
	return err
}

func (c *configCmd) deviceGet(config *lxd.Config, which string, args []string) error {
	if len(args) < 5 {
		return errArgs
	}

	remote, name := config.ParseRemoteAndContainer(args[2])

	client, err := lxd.NewClient(config, remote)
	if err != nil {
		return err
	}

	devname := args[3]
	key := args[4]

	if which == "profile" {
		st, err := client.ProfileConfig(name)
		if err != nil {
			return err
		}

		dev, ok := st.Devices[devname]
		if !ok {
			return fmt.Errorf(i18n.G("The device doesn't exist"))
		}

		fmt.Println(dev[key])
	} else {
		st, err := client.ContainerInfo(name)
		if err != nil {
			return err
		}

		dev, ok := st.Devices[devname]
		if !ok {
			return fmt.Errorf(i18n.G("The device doesn't exist"))
		}

		fmt.Println(dev[key])
	}

	return nil
}

func (c *configCmd) deviceSet(config *lxd.Config, which string, args []string) error {
	if len(args) < 6 {
		return errArgs
	}

	remote, name := config.ParseRemoteAndContainer(args[2])

	client, err := lxd.NewClient(config, remote)
	if err != nil {
		return err
	}

	devname := args[3]
	key := args[4]
	value := args[5]

	if which == "profile" {
		st, err := client.ProfileConfig(name)
		if err != nil {
			return err
		}

		dev, ok := st.Devices[devname]
		if !ok {
			return fmt.Errorf(i18n.G("The device doesn't exist"))
		}

		dev[key] = value
		st.Devices[devname] = dev

		err = client.PutProfile(name, st.Writable())
		if err != nil {
			return err
		}
	} else {
		st, err := client.ContainerInfo(name)
		if err != nil {
			return err
		}

		dev, ok := st.Devices[devname]
		if !ok {
			return fmt.Errorf(i18n.G("The device doesn't exist"))
		}

		dev[key] = value
		st.Devices[devname] = dev

		err = client.UpdateContainerConfig(name, st.Writable())
		if err != nil {
			return err
		}
	}

	return err
}

func (c *configCmd) deviceUnset(config *lxd.Config, which string, args []string) error {
	if len(args) < 5 {
		return errArgs
	}

	remote, name := config.ParseRemoteAndContainer(args[2])

	client, err := lxd.NewClient(config, remote)
	if err != nil {
		return err
	}

	devname := args[3]
	key := args[4]

	if which == "profile" {
		st, err := client.ProfileConfig(name)
		if err != nil {
			return err
		}

		dev, ok := st.Devices[devname]
		if !ok {
			return fmt.Errorf(i18n.G("The device doesn't exist"))
		}

		delete(dev, key)
		st.Devices[devname] = dev

		err = client.PutProfile(name, st.Writable())
		if err != nil {
			return err
		}
	} else {
		st, err := client.ContainerInfo(name)
		if err != nil {
			return err
		}

		dev, ok := st.Devices[devname]
		if !ok {
			return fmt.Errorf(i18n.G("The device doesn't exist"))
		}

		delete(dev, key)
		st.Devices[devname] = dev

		err = client.UpdateContainerConfig(name, st.Writable())
		if err != nil {
			return err
		}
	}

	return err
}

func (c *configCmd) deviceRm(config *lxd.Config, which string, args []string) error {
	if len(args) < 4 {
		return errArgs
	}
	remote, name := config.ParseRemoteAndContainer(args[2])

	client, err := lxd.NewClient(config, remote)
	if err != nil {
		return err
	}

	devname := args[3]
	var resp *api.Response
	if which == "profile" {
		resp, err = client.ProfileDeviceDelete(name, devname)
	} else {
		resp, err = client.ContainerDeviceDelete(name, devname)
	}
	if err != nil {
		return err
	}
	if which != "profile" {
		err = client.WaitForSuccess(resp.Operation)
	}
	if err == nil {
		fmt.Printf(i18n.G("Device %s removed from %s")+"\n", devname, name)
	}
	return err
}

func (c *configCmd) deviceList(config *lxd.Config, which string, args []string) error {
	if len(args) < 3 {
		return errArgs
	}
	remote, name := config.ParseRemoteAndContainer(args[2])

	client, err := lxd.NewClient(config, remote)
	if err != nil {
		return err
	}

	var resp []string
	if which == "profile" {
		resp, err = client.ProfileListDevices(name)
	} else {
		resp, err = client.ContainerListDevices(name)
	}
	if err != nil {
		return err
	}
	fmt.Printf("%s\n", strings.Join(resp, "\n"))

	return nil
}

func (c *configCmd) deviceShow(config *lxd.Config, which string, args []string) error {
	if len(args) < 3 {
		return errArgs
	}
	remote, name := config.ParseRemoteAndContainer(args[2])

	client, err := lxd.NewClient(config, remote)
	if err != nil {
		return err
	}

	var devices map[string]map[string]string
	if which == "profile" {
		resp, err := client.ProfileConfig(name)
		if err != nil {
			return err
		}

		devices = resp.Devices
	} else {
		resp, err := client.ContainerInfo(name)
		if err != nil {
			return err
		}

		devices = resp.Devices
	}

	data, err := yaml.Marshal(&devices)
	if err != nil {
		return err
	}

	fmt.Printf(string(data))

	return nil
}
