package toolkit

import (
	"bytes"
	"crypto/rand"
	"fmt"
	"github.com/brudnak/ha-upgraded-rancher-k3s/tools/hcl"
	"golang.org/x/crypto/ssh"
	"log"
	"net"
	"os"
	"strings"
	"time"

	"github.com/spf13/viper"
)

const randomStringSource = "abcdefghijklmnopqrstuvwxyz"

type Tools struct{}

type K3SConfig struct {
	DBPassword string
	DBEndpoint string
	RancherURL string
	Node1IP    string
	Node2IP    string
}

func (t *Tools) RandomString(n int) string {
	s, r := make([]rune, n), []rune(randomStringSource)
	for i := range s {
		p, _ := rand.Prime(rand.Reader, len(r))
		x, y := p.Uint64(), uint64(len(r))
		s[i] = r[x%y]
	}
	return string(s)
}

func (t *Tools) WaitForNodeReady(nodeIP string) error {
	timeout := time.After(5 * time.Minute)
	poll := time.Tick(10 * time.Second)

	for {
		select {
		case <-timeout:
			return fmt.Errorf("timed out waiting for node to become ready")
		case <-poll:
			// Check the K3S service status.
			nodeStatus, err := t.RunCommand("systemctl is-active k3s", nodeIP)
			if err != nil {
				return fmt.Errorf("failed to check node status: %w", err)
			}

			// If the K3S service is running (i.e., the status is "active"), return nil.
			if strings.TrimSpace(nodeStatus) == "active" {
				return nil
			}
		}
	}
}

func (t *Tools) HAInstallK3S(config K3SConfig) string {

	k3sVersion := viper.GetString("k3s.version")

	nodeOneCommand := nodeCommandBuilder(k3sVersion, "SECRET", config.DBPassword, config.DBEndpoint, config.RancherURL, config.Node1IP)

	_, err := t.RunCommand(nodeOneCommand, config.Node1IP)
	if err != nil {
		log.Println(err)
	}

	token, err := t.RunCommand("sudo cat /var/lib/rancher/k3s/server/token", config.Node1IP)
	if err != nil {
		log.Println(err)
	}

	serverKubeConfig, err := t.RunCommand("sudo cat /etc/rancher/k3s/k3s.yaml", config.Node1IP)
	if err != nil {
		log.Println(err)
	}

	// Wait for node one to be ready
	err = t.WaitForNodeReady(config.Node1IP)
	if err != nil {
		log.Println("node one is not ready: %w", err)
	}

	nodeTwoCommand := nodeCommandBuilder(k3sVersion, token, config.DBPassword, config.DBEndpoint, config.RancherURL, config.Node2IP)
	_, err = t.RunCommand(nodeTwoCommand, config.Node2IP)
	if err != nil {
		log.Println(err)
	}

	// Wait for node two to be ready
	err = t.WaitForNodeReady(config.Node2IP)
	if err != nil {
		log.Println("node two is not ready: %w", err)
	}

	kubeConf := []byte(serverKubeConfig)

	configIP := fmt.Sprintf("https://%s:6443", config.Node1IP)
	output := bytes.Replace(kubeConf, []byte("https://127.0.0.1:6443"), []byte(configIP), -1)

	err = os.WriteFile("../../ha.yml", output, 0644)
	if err != nil {
		log.Println("failed creating ha config:", err)
	}

	// Initial terraform variable file
	initialFilePath := "../modules/helm/ha/terraform.tfvars"
	hcl.RancherHelm(
		config.RancherURL,
		viper.GetString("rancher.repository_url"),
		viper.GetString("rancher.bootstrap_password"),
		viper.GetString("rancher.version"),
		viper.GetString("rancher.image_tag"),
		initialFilePath,
		viper.GetBool("rancher.psp_bool"),
	)

	// Upgrade terraform variable file
	upgradeFilePath := "../modules/helm/ha/upgrade.tfvars"
	hcl.RancherHelm(
		config.RancherURL,
		viper.GetString("rancher.repository_url"),
		viper.GetString("rancher.bootstrap_password"),
		viper.GetString("upgrade.version"),
		viper.GetString("upgrade.image_tag"),
		upgradeFilePath,
		viper.GetBool("rancher.psp_bool"))
	return configIP
}

func (t *Tools) RunCommand(cmd string, pubIP string) (string, error) {

	pemKey := viper.GetString("aws.rsa_private_key")

	dialIP := fmt.Sprintf("%s:22", pubIP)

	signer, err := ssh.ParsePrivateKey([]byte(pemKey))
	if err != nil {
		return "", fmt.Errorf("failed to parse private key: %w", err)
	}
	config := &ssh.ClientConfig{
		User:            "ubuntu",
		Auth:            []ssh.AuthMethod{ssh.PublicKeys(signer)},
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
	}
	conn, err := ssh.Dial("tcp", dialIP, config)
	if err != nil {
		return "", fmt.Errorf("failed to establish ssh connection: %w", err)
	}
	defer func() {
		if err := conn.Close(); err != nil {
			log.Println(err)
		}
	}()

	session, err := conn.NewSession()
	if err != nil {
		return "", fmt.Errorf("failed to create new ssh session: %w", err)
	}
	defer func() {
		if err := session.Close(); err != nil {
			log.Println(err)
		}
	}()

	var stdoutBuf bytes.Buffer
	session.Stdout = &stdoutBuf
	err = session.Run(cmd)
	if err != nil {
		return "", fmt.Errorf("failed to run ssh command: %w", err)
	}

	stringOut := stdoutBuf.String()
	stringOut = strings.TrimRight(stringOut, "\r\n")

	return stringOut, nil
}

func (t *Tools) CheckIPAddress(ip string) string {
	if net.ParseIP(ip) == nil {
		return "invalid"
	} else {
		return "valid"
	}
}

func (t *Tools) RemoveFile(filePath string) error {
	err := os.Remove(filePath)
	if err != nil {
		return fmt.Errorf("error removing file %s: %w", filePath, err)
	}
	return nil
}

func (t *Tools) RemoveFolder(folderPath string) error {
	err := os.RemoveAll(folderPath)
	if err != nil {
		return fmt.Errorf("error removing folder %s: %w", folderPath, err)
	}
	return nil
}

func nodeCommandBuilder(version, secret, password, endpoint, url, ip string) string {
	return fmt.Sprintf(`curl -sfL https://get.k3s.io | INSTALL_K3S_VERSION='%s' sh -s - server --token=%s --datastore-endpoint='mysql://tfadmin:%s@tcp(%s)/k3s' --tls-san %s --node-external-ip %s`, version, secret, password, endpoint, url, ip)
}
