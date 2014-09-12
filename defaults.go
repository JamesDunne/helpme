// +build !bit

package main

var DefaultSSHConnection = SSHConnection{
	Host: "",
	User: "",
	Auth: []SSHAuthMethod{
		SSHAuthMethod{Kind: SSHAuthPassword, Data: ""},
	},
}
