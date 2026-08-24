// Package helpers provides test utilities for integration tests.
package helpers

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
	"time"

	clientv3 "go.etcd.io/etcd/client/v3"
)

// ComposeProject is the docker compose project the integration stack runs under.
//
// Empty by default, and deliberately: the Makefile starts the stack without -p,
// so compose derives the project from the directory. Commands here run from the
// same directory and inherit the same derivation. Set TEST_COMPOSE_PROJECT only
// when the stack was started with an explicit name.
func ComposeProject() string {
	return getEnv("TEST_COMPOSE_PROJECT", "")
}

// ComposeFiles is the -f arguments used to reach the running stack.
func ComposeFiles() []string {
	raw := getEnv("TEST_COMPOSE_FILES", "docker-compose.yml,deployments/docker-compose.test.yml")
	var args []string
	for _, f := range strings.Split(raw, ",") {
		if f = strings.TrimSpace(f); f != "" {
			args = append(args, "-f", f)
		}
	}
	return args
}

// ComposeRun runs a docker compose subcommand against the integration stack and
// returns its combined output.
func ComposeRun(ctx context.Context, args ...string) (string, error) {
	full := []string{"compose"}
	if project := ComposeProject(); project != "" {
		full = append(full, "-p", project)
	}
	full = append(full, ComposeFiles()...)
	full = append(full, args...)

	cmd := exec.CommandContext(ctx, "docker", full...)
	cmd.Dir = RepoRoot()
	out, err := cmd.CombinedOutput()
	if err != nil {
		return string(out), fmt.Errorf("docker %s: %w: %s", strings.Join(full, " "), err, out)
	}
	return string(out), nil
}

// RepoRoot is the directory docker compose commands run from.
func RepoRoot() string {
	return getEnv("TEST_REPO_ROOT", "../..")
}

// RestartService restarts one compose service and returns when the restart
// command has completed.
func RestartService(ctx context.Context, service string) error {
	_, err := ComposeRun(ctx, "restart", service)
	return err
}

// StopService stops one compose service without starting it again, which is how
// a test observes what shutdown left behind.
func StopService(ctx context.Context, service string) error {
	_, err := ComposeRun(ctx, "stop", service)
	return err
}

// StartService starts a stopped compose service.
func StartService(ctx context.Context, service string) error {
	_, err := ComposeRun(ctx, "start", service)
	return err
}

// EtcdClient connects to the etcd the stack registers with.
func EtcdClient(cfg *TestConfig) (*clientv3.Client, error) {
	return clientv3.New(clientv3.Config{
		Endpoints:   []string{cfg.EtcdAddress},
		DialTimeout: 5 * time.Second,
	})
}

// EtcdKeys returns the keys registered under a prefix.
func EtcdKeys(ctx context.Context, cli *clientv3.Client, prefix string) ([]string, error) {
	resp, err := cli.Get(ctx, prefix, clientv3.WithPrefix(), clientv3.WithKeysOnly())
	if err != nil {
		return nil, err
	}
	keys := make([]string, 0, len(resp.Kvs))
	for _, kv := range resp.Kvs {
		keys = append(keys, string(kv.Key))
	}
	return keys, nil
}

// ConnectServicePath is the etcd prefix connect instances register under.
const ConnectServicePath = "/gochat_srv/ConnectRpc"

// LogicServicePath is the etcd prefix logic instances register under.
const LogicServicePath = "/gochat_srv/LogicRpc"
