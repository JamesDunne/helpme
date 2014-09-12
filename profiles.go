package main

type PortForward struct {
	LocalAddr  string
	RemoteAddr string
}

type SSHAuthKind int

const (
	SSHAuthPassword SSHAuthKind = iota
	SSHAuthPublicKey
)

type SSHAuthMethod struct {
	Kind SSHAuthKind
	Data string
}

type SSHConnection struct {
	Host string
	User string
	Auth []SSHAuthMethod
}

type Profile struct {
	SSH           *SSHConnection
	LocalToRemote []PortForward
	RemoteToLocal []PortForward
}

var DefaultProfiles = map[string]*Profile{
	"rdp_server": &Profile{
		SSH: &DefaultSSHConnection,
		RemoteToLocal: []PortForward{
			PortForward{LocalAddr: "127.0.0.1:3389", RemoteAddr: "127.0.0.1:3391"},
		},
	},
	"rdp_client": &Profile{
		SSH: &DefaultSSHConnection,
		LocalToRemote: []PortForward{
			PortForward{LocalAddr: "127.0.0.1:3391", RemoteAddr: "127.0.0.1:3391"},
		},
	},
}
