package containercontract

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

type composeFile struct {
	Name     string                    `yaml:"name"`
	Services map[string]composeService `yaml:"services"`
	Networks map[string]any            `yaml:"networks"`
	Volumes  map[string]any            `yaml:"volumes"`
}

type composeService struct {
	Image             string            `yaml:"image"`
	Init              bool              `yaml:"init"`
	NetworkMode       string            `yaml:"network_mode"`
	Networks          []string          `yaml:"networks"`
	Privileged        bool              `yaml:"privileged"`
	CapDrop           []string          `yaml:"cap_drop"`
	CapAdd            []string          `yaml:"cap_add"`
	DeviceCgroupRules []string          `yaml:"device_cgroup_rules"`
	Volumes           []map[string]any  `yaml:"volumes"`
	Environment       map[string]string `yaml:"environment"`
	DependsOn         map[string]any    `yaml:"depends_on"`
	Healthcheck       map[string]any    `yaml:"healthcheck"`
	SecurityOpt       []string          `yaml:"security_opt"`
	Sysctls           map[string]string `yaml:"sysctls"`
	Ports             []string          `yaml:"ports"`
	ReadOnly          bool              `yaml:"read_only"`
	Restart           string            `yaml:"restart"`
	PidsLimit         int               `yaml:"pids_limit"`
	StopGracePeriod   string            `yaml:"stop_grace_period"`
	Tmpfs             []string          `yaml:"tmpfs"`
	GroupAdd          []string          `yaml:"group_add"`
	User              string            `yaml:"user"`
	EntryPoint        []string          `yaml:"entrypoint"`
	Command           []string          `yaml:"command"`
}

func repositoryRoot(t *testing.T) string {
	t.Helper()
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	return root
}

func readCompose(t *testing.T) composeFile {
	t.Helper()
	body, err := os.ReadFile(filepath.Join(repositoryRoot(t), "compose.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	var value composeFile
	decoder := yaml.NewDecoder(strings.NewReader(string(body)))
	decoder.KnownFields(true)
	if err := decoder.Decode(&value); err != nil {
		t.Fatalf("decode compose.yaml: %v", err)
	}
	return value
}

func TestComposePreservesThreeProcessPrivilegeBoundaries(t *testing.T) {
	compose := readCompose(t)
	for _, name := range []string{"data-init", "agent", "netd", "app", "bootstrap"} {
		if _, found := compose.Services[name]; !found {
			t.Fatalf("compose service %q is missing", name)
		}
	}
	for name, service := range compose.Services {
		if service.Privileged {
			t.Fatalf("service %q enables privileged mode", name)
		}
		if !service.ReadOnly {
			t.Fatalf("service %q does not use a read-only root filesystem", name)
		}
		if !contains(service.CapDrop, "ALL") {
			t.Fatalf("service %q does not drop the default capability set", name)
		}
	}

	agent := compose.Services["agent"]
	if agent.NetworkMode != "none" || len(agent.Networks) != 0 {
		t.Fatalf("Agent has a network attachment: %#v", agent)
	}
	if agent.User != "0:10001" {
		t.Fatalf("Agent bootstrap and health-check identity = %q", agent.User)
	}
	if agent.Environment["SIMPLUS_AGENT_SOCKET"] != "/run/simplus-agent/simplus-agent.sock" {
		t.Fatalf("Agent CLI socket environment = %#v", agent.Environment)
	}
	if agent.Environment["SIMPLUS_AGENT_STATE_ROOT"] != "/var/lib/simplus-agent/qdc507-sms" {
		t.Fatalf("Agent SMS state root environment = %#v", agent.Environment)
	}
	if !reflect.DeepEqual(agent.DeviceCgroupRules, []string{"c 188:* rw"}) {
		t.Fatalf("Agent device cgroup rules = %#v", agent.DeviceCgroupRules)
	}
	if !reflect.DeepEqual(agent.CapAdd, []string{"CHOWN", "SETGID", "SETPCAP", "SETUID"}) {
		t.Fatalf("Agent capability set = %#v", agent.CapAdd)
	}
	assertBindMount(t, agent, "/sys/bus/usb/devices", "/host/sys/bus/usb/devices", true, true)
	assertBindMount(t, agent, "/sys/devices", "/host/sys/devices", true, true)
	assertBindMount(t, agent, "/dev", "/host/dev", false, true)
	assertBindMount(t, agent, "/sys/bus/usb-serial/drivers/option1/new_id", "/host/sys/bus/usb-serial/drivers/option1/new_id", false, true)

	netd := compose.Services["netd"]
	if netd.NetworkMode == "host" || !reflect.DeepEqual(netd.Networks, []string{"runtime"}) {
		t.Fatalf("netd network boundary = mode %q networks %#v", netd.NetworkMode, netd.Networks)
	}
	expectedNetdCapabilities := []string{"DAC_OVERRIDE", "KILL", "SETGID", "SETUID", "NET_ADMIN", "NET_RAW", "NET_BIND_SERVICE", "SYS_ADMIN"}
	if !reflect.DeepEqual(netd.CapAdd, expectedNetdCapabilities) {
		t.Fatalf("netd capability set = %#v", netd.CapAdd)
	}
	if netd.Sysctls["net.ipv4.ip_forward"] != "1" {
		t.Fatalf("netd forwarding sysctl = %#v", netd.Sysctls)
	}
	if !reflect.DeepEqual(netd.Ports, []string{"0.0.0.0:${SIMPLUS_CONTROLLER_PORT:-19090}:19090"}) {
		t.Fatalf("netd published ports = %#v", netd.Ports)
	}

	app := compose.Services["app"]
	if len(app.CapAdd) != 0 || app.User != "10001:10001" || !reflect.DeepEqual(app.Networks, []string{"runtime"}) {
		t.Fatalf("app privilege boundary = %#v", app)
	}
	if app.Environment["SIMPLUS_LISTEN_ADDR"] != "0.0.0.0:8080" || app.Environment["SIMPLUS_BACKEND"] != "hardware" {
		t.Fatalf("app environment = %#v", app.Environment)
	}
	if !reflect.DeepEqual(app.Ports, []string{"0.0.0.0:${SIMPLUS_HTTP_PORT:-8080}:8080"}) {
		t.Fatalf("app published ports = %#v", app.Ports)
	}
	dataInit := compose.Services["data-init"]
	if dataInit.User != "0:0" || !reflect.DeepEqual(dataInit.CapAdd, []string{"CHOWN", "DAC_OVERRIDE", "FOWNER"}) || dataInit.NetworkMode != "none" {
		t.Fatalf("data-init privilege boundary = %#v", dataInit)
	}
	bootstrap := compose.Services["bootstrap"]
	if bootstrap.User != "0:0" || !reflect.DeepEqual(bootstrap.CapAdd, []string{"DAC_OVERRIDE"}) || bootstrap.NetworkMode != "none" {
		t.Fatalf("bootstrap privilege boundary = %#v", bootstrap)
	}
}

func TestSourceComposeRequiresExplicitDevelopmentTagAndRuntimeVolumes(t *testing.T) {
	compose := readCompose(t)
	for _, name := range []string{"data-init", "agent", "netd", "app", "bootstrap"} {
		image := compose.Services[name].Image
		if strings.HasSuffix(image, ":latest") || !strings.Contains(image, "${SIMPLUS_IMAGE_TAG:?set SIMPLUS_IMAGE_TAG for source-tree development validation}") {
			t.Fatalf("service %q does not require the explicit source-development image tag: %q", name, image)
		}
		if len(compose.Services[name].Healthcheck) == 0 && name != "data-init" && name != "bootstrap" {
			t.Fatalf("service %q has no typed healthcheck", name)
		}
	}
	for _, name := range []string{"agent-runtime", "netd-runtime"} {
		if _, found := compose.Volumes[name]; !found {
			t.Fatalf("runtime volume %q is missing", name)
		}
	}
	if _, found := compose.Networks["runtime"]; !found {
		t.Fatal("runtime bridge network is missing")
	}
}

func TestDockerfileHasOnlyProductionRuntimeTargets(t *testing.T) {
	body, err := os.ReadFile(filepath.Join(repositoryRoot(t), "Dockerfile"))
	if err != nil {
		t.Fatal(err)
	}
	text := string(body)
	for _, target := range []string{" AS control", " AS agent", " AS netd"} {
		if !strings.Contains(text, target) {
			t.Fatalf("Dockerfile target %q is missing", strings.TrimSpace(target))
		}
	}
	for _, label := range []string{
		`org.opencontainers.image.source="https://github.com/leonfox28/simplus"`,
		`org.opencontainers.image.version="$VERSION"`,
		`org.opencontainers.image.revision="$COMMIT"`,
		`org.opencontainers.image.licenses="LicenseRef-PolyForm-Noncommercial-1.0.0"`,
	} {
		if !strings.Contains(text, label) {
			t.Fatalf("Dockerfile is missing OCI metadata label %q", label)
		}
	}
	for _, forbidden := range []string{" AS dev", "network_mode: host", "privileged: true"} {
		if strings.Contains(text, forbidden) {
			t.Fatalf("Dockerfile contains forbidden development/privileged contract %q", forbidden)
		}
	}
}

func TestContainerHostCheckRejectsActiveOrEnabledLegacyServices(t *testing.T) {
	body, err := os.ReadFile(filepath.Join(repositoryRoot(t), "scripts", "release", "check-container-host.sh"))
	if err != nil {
		t.Fatal(err)
	}
	text := string(body)
	for _, required := range []string{
		"for service in simplus-ml307a-bind.service simplus-agent.service simplus-netd.service simplusd.service simplus-agent-dev.service",
		`systemctl is-active --quiet "$service" || systemctl is-enabled --quiet "$service"`,
		"active or enabled; stop and disable the legacy/development service explicitly before starting Compose",
	} {
		if !strings.Contains(text, required) {
			t.Fatalf("container host check is missing legacy-service conflict contract %q", required)
		}
	}
}

func TestNetdImagePinsMihomoAndReleaseSource(t *testing.T) {
	dockerfile, err := os.ReadFile(filepath.Join(repositoryRoot(t), "Dockerfile"))
	if err != nil {
		t.Fatal(err)
	}
	text := string(dockerfile)
	for _, required := range []string{
		"MIHOMO_VERSION=v1.19.29",
		"5612e698e96c8b8ad15abc4c0a4f098eba9234354b4f248cb97f2528e215b094",
		"bd2a08ae155b7dffc12a1bdf610ff5f17c45058414a1d2c562e28eb9309abff6",
		"COPY --from=mihomo-fetch /out/ /usr/share/simplus/mihomo/",
		"/usr/share/common-licenses/GPL-3",
	} {
		if !strings.Contains(text, required) {
			t.Fatalf("Dockerfile is missing pinned Mihomo contract %q", required)
		}
	}
	source, err := os.ReadFile(filepath.Join(repositoryRoot(t), "third_party", "mihomo", "SOURCE"))
	if err != nil {
		t.Fatal(err)
	}
	for _, required := range []string{
		"version=v1.19.29",
		"source_commit=e26714a181ac0e2fa803453c0a8e9a9ce94e31cb",
		"corresponding_source_archive=mihomo-v1.19.29-source.tar.gz",
		"corresponding_source_sha256=2b0a69526ad551887591386c7934badd1f87d75c4e6a3a399170fcc9613acde5",
	} {
		if !strings.Contains(string(source), required) {
			t.Fatalf("Mihomo source metadata is missing %q", required)
		}
	}
}

func TestContainerWorkflowPublishesOnlyStrictVersionTagsAfterReleaseAssets(t *testing.T) {
	body, err := os.ReadFile(filepath.Join(repositoryRoot(t), ".github", "workflows", "containers.yml"))
	if err != nil {
		t.Fatal(err)
	}
	text := string(body)
	for _, required := range []string{
		`[[ $GITHUB_EVENT_NAME == push ]]`,
		`[[ $GITHUB_REPOSITORY == leonfox28/simplus ]]`,
		`^v[0-9]+\.[0-9]+\.[0-9]+$`,
		"concurrency:",
		"cancel-in-progress: false",
		"verify-images:",
		"push: false",
		"publish-images:",
		"needs: [metadata, compose-contract, publish-release-sources]",
		"packages: write",
		"push-by-digest=true",
		"scripts/release/inspect-published-container-image.sh",
		"--head --output /dev/null --write-out '%{http_code}'",
		"refusing to move an existing immutable image tag",
		"published version tag digest differs from the staged immutable digest",
		"Validate the exact public Release asset allowlist",
		"--prerelease",
		"simplus-images-${RELEASE_TAG}.json",
		"provenance: mode=max",
		"sbom: true",
		"platforms: linux/amd64",
		"ghcr.io/leonfox28/simplus-${{ matrix.target }}",
	} {
		if !strings.Contains(text, required) {
			t.Fatalf("container workflow is missing release contract %q", required)
		}
	}
	if count := strings.Count(text, "--head --output /dev/null --write-out '%{http_code}'"); count != 2 {
		t.Fatalf("container workflow has %d registry HEAD probes, want 2", count)
	}
	for _, forbidden := range []string{
		"gh release upload \"$RELEASE_TAG\" release-assets/* --clobber",
		"tags: latest",
		"tags: main",
		"--request HEAD",
	} {
		if strings.Contains(text, forbidden) {
			t.Fatalf("container workflow contains mutable publication contract %q", forbidden)
		}
	}
	if strings.Index(text, "publish-release-sources:") > strings.Index(text, "publish-images:") {
		t.Fatal("release source publication must be declared before image publication")
	}
	publishStart := strings.Index(text, "  publish-images:")
	publishEnd := strings.Index(text, "  publish-image-manifest:")
	if publishStart == -1 || publishEnd <= publishStart {
		t.Fatal("cannot locate the image publication job")
	}
	publishJob := text[publishStart:publishEnd]
	if strings.Contains(publishJob, "\n          push: true") || strings.Contains(publishJob, "\n          tags: ghcr.io/") {
		t.Fatal("image publication must stage content by digest instead of pushing the version tag during build")
	}
}

func TestDataInitKeepsPrivateFixedUIDStateAndDoesNotOverwriteCore(t *testing.T) {
	body, err := os.ReadFile(filepath.Join(repositoryRoot(t), "containers", "data-init.sh"))
	if err != nil {
		t.Fatal(err)
	}
	text := string(body)
	for _, required := range []string{
		`prepare_directory "$root/core" 10001 10001 0700`,
		`prepare_directory "$root/agent" 10002 10002 0700`,
		`[ ! -L "$core_manifest" ] && [ -f "$core_manifest" ]`,
		`refusing to guess active state`,
		`chmod 0600 "$manifest_tmp"`,
	} {
		if !strings.Contains(text, required) {
			t.Fatalf("data-init contract is missing %q", required)
		}
	}
	if strings.Contains(text, `rm -f -- "$core_manifest"`) || strings.Contains(text, `rm -rf -- "$mihomo_root"`) {
		t.Fatal("data-init contains a Mihomo current-manifest overwrite path")
	}
}

func TestAgentEntrypointInitializesRuntimeVolumeWithoutFowner(t *testing.T) {
	body, err := os.ReadFile(filepath.Join(repositoryRoot(t), "containers", "agent-entrypoint.sh"))
	if err != nil {
		t.Fatal(err)
	}
	text := string(body)
	rootOwner := strings.Index(text, "chown 0:0 /run/simplus-agent")
	mode := strings.Index(text, "chmod 0750 /run/simplus-agent")
	agentOwner := strings.Index(text, "chown 10002:10001 /run/simplus-agent")
	if rootOwner < 0 || mode < 0 || agentOwner < 0 || !(rootOwner < mode && mode < agentOwner) {
		t.Fatal("Agent runtime initialization must temporarily restore root ownership before chmod and final handoff")
	}
	for _, required := range []string{
		"--state-root /var/lib/simplus-agent/qdc507-sms",
		"--identity-key /var/lib/simplus-agent/.identity-key-v1",
	} {
		if !strings.Contains(text, required) {
			t.Fatalf("Agent entrypoint is missing production state contract %q", required)
		}
	}
	compose := readCompose(t)
	if contains(compose.Services["agent"].CapAdd, "FOWNER") {
		t.Fatal("Agent must not gain CAP_FOWNER solely to initialize its runtime volume")
	}
}

func TestNetdPreflightExercisesDisposableKernelObjects(t *testing.T) {
	body, err := os.ReadFile(filepath.Join(repositoryRoot(t), "containers", "netd-preflight.sh"))
	if err != nil {
		t.Fatal(err)
	}
	text := string(body)
	for _, required := range []string{
		`ip netns add "$namespace"`,
		`type veth peer`,
		`ip xfrm state add`,
		`ip xfrm policy add`,
		`tproxy ip to 127.0.0.1:19999`,
		`trap cleanup 0 1 2 15`,
	} {
		if !strings.Contains(text, required) {
			t.Fatalf("netd preflight is missing disposable kernel probe %q", required)
		}
	}
}

func TestDockerBuildContextExcludesPrivateRuntimeState(t *testing.T) {
	body, err := os.ReadFile(filepath.Join(repositoryRoot(t), ".dockerignore"))
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(string(body), "\n")
	for _, required := range []string{".git", ".dev", ".tools", ".env", "/data", "ref/*", "docs/private", "*.pcap"} {
		if !contains(lines, required) {
			t.Fatalf(".dockerignore does not exclude %q", required)
		}
	}
	if contains(lines, "dist") {
		t.Fatal("unanchored dist rule would remove bundled Zashboard assets")
	}
}

func assertBindMount(t *testing.T, service composeService, source, target string, readOnly, noCreate bool) {
	t.Helper()
	for _, mount := range service.Volumes {
		if mount["type"] != "bind" || mount["source"] != source || mount["target"] != target {
			continue
		}
		if value, _ := mount["read_only"].(bool); value != readOnly {
			t.Fatalf("mount %s read_only = %#v", source, mount["read_only"])
		}
		bind, _ := mount["bind"].(map[string]any)
		if noCreate {
			if value, _ := bind["create_host_path"].(bool); value {
				t.Fatalf("mount %s allows host path creation", source)
			}
			if _, found := bind["create_host_path"]; !found {
				t.Fatalf("mount %s does not explicitly reject host path creation", source)
			}
		}
		return
	}
	t.Fatalf("bind mount %s -> %s is missing", source, target)
}

func contains(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}
