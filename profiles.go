package main

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	//"log"
)

type PortForward struct {
	LocalAddr  string `json:"local"`
	RemoteAddr string `json:"remote"`
}

type SSHAuthKind int

const (
	SSHAuthPassword SSHAuthKind = iota
	SSHAuthPublicKey
)

type json_SSHAuthMethod struct {
	Kind string `json:"kind"`
	Data string `json:"data"`
}

type SSHAuthMethod struct {
	Kind SSHAuthKind
	Data string
}

func (auth *SSHAuthMethod) UnmarshalJSON(jsonbytes []byte) (err error) {
	jd := json_SSHAuthMethod{}
	err = json.Unmarshal(jsonbytes, &jd)
	if err != nil {
		return
	}

	switch jd.Kind {
	case "password":
		auth.Kind = SSHAuthPassword
	case "publickey":
		auth.Kind = SSHAuthPublicKey
	}
	auth.Data = jd.Data

	return
}

type SSHConnection struct {
	Host string          `json:"host"`
	User string          `json:"user"`
	Auth []SSHAuthMethod `json:"auth"`
}

type Profile struct {
	IsDefault     bool           `json:"isDefault"`
	SSH           *SSHConnection `json:"ssh"`
	LocalToRemote []PortForward  `json:"localToRemote"`
	RemoteToLocal []PortForward  `json:"remoteToLocal"`
}

var DefaultProfiles = map[string]*Profile{
	"rdp_server": &Profile{
		RemoteToLocal: []PortForward{
			PortForward{LocalAddr: "127.0.0.1:3389", RemoteAddr: "127.0.0.1:3391"},
		},
	},
	"rdp_client": &Profile{
		LocalToRemote: []PortForward{
			PortForward{LocalAddr: "127.0.0.1:3391", RemoteAddr: "127.0.0.1:3391"},
		},
	},
}

func LoadProfiles(path string) (profiles map[string]*Profile, defaultProfileName string, err error) {
	// Copy in default profiles:
	profiles = make(map[string]*Profile)
	for key, value := range DefaultProfiles {
		profiles[key] = value
	}

	// Load the JSON file:
	filebytes, err := ioutil.ReadFile(path)
	if err != nil {
		return
	}
	// Decode JSON profiles:
	err = json.Unmarshal(filebytes, &profiles)
	if err != nil {
		return
	}

	// Verify only one default:
	defaultProfileName = "rdp_server"
	defaultProfile := (*Profile)(nil)
	for name, prof := range profiles {
		if prof.IsDefault {
			if defaultProfile == nil {
				defaultProfile = prof
				defaultProfileName = name
			} else {
				err = fmt.Errorf("Cannot have multiple default profiles!")
				return
			}
		}
	}

	return
}
