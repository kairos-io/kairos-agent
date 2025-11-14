package config

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"unicode"

	"github.com/joho/godotenv"
	version "github.com/kairos-io/kairos-agent/v2/internal/common"
	"github.com/kairos-io/kairos-agent/v2/pkg/cloudinit"
	"github.com/kairos-io/kairos-agent/v2/pkg/constants"
	"github.com/kairos-io/kairos-agent/v2/pkg/implementations/http"
	"github.com/kairos-io/kairos-agent/v2/pkg/implementations/imageextractor"
	runner2 "github.com/kairos-io/kairos-agent/v2/pkg/implementations/runner"
	"github.com/kairos-io/kairos-agent/v2/pkg/implementations/syscall"
	"github.com/kairos-io/kairos-sdk/collector"
	"github.com/kairos-io/kairos-sdk/schema"
	"github.com/kairos-io/kairos-sdk/state"
	"github.com/kairos-io/kairos-sdk/types/cloudinitrunner"
	"github.com/kairos-io/kairos-sdk/types/config"
	sdkFs "github.com/kairos-io/kairos-sdk/types/fs"
	sdkHttp "github.com/kairos-io/kairos-sdk/types/http"
	sdkImages "github.com/kairos-io/kairos-sdk/types/images"
	"github.com/kairos-io/kairos-sdk/types/install"
	"github.com/kairos-io/kairos-sdk/types/logger"
	"github.com/kairos-io/kairos-sdk/types/platform"
	"github.com/kairos-io/kairos-sdk/types/runner"
	sdkSyscall "github.com/kairos-io/kairos-sdk/types/syscall"
	yip "github.com/mudler/yip/pkg/schema"
	"github.com/sanity-io/litter"
	"github.com/spf13/viper"
	"github.com/twpayne/go-vfs/v5"
	"gopkg.in/yaml.v3"
	"k8s.io/mount-utils"
)

const (
	DefaultWebUIListenAddress = ":8080"
)

type Config struct {
	config.Config
}

func NewConfig(opts ...GenericOptions) *Config {
	log := logger.NewKairosLogger("agent", "info", false)
	// Get the viper config in case something in command line or env var has set it and set the level asap
	if viper.GetBool("debug") {
		log.SetLevel("debug")
	}

	hostPlatform, err := platform.NewPlatformFromArch(runtime.GOARCH)
	if err != nil {
		log.Errorf("error parsing default platform (%s): %s", runtime.GOARCH, err.Error())
		return nil
	}

	arch, err := golangArchToArch(runtime.GOARCH)
	if err != nil {
		log.Errorf("invalid arch: %s", err.Error())
		return nil
	}

	c := &Config{
		Config: config.Config{
			Fs:                        vfs.OSFS,
			Logger:                    log,
			Syscall:                   &syscall.RealSyscall{},
			Client:                    http.NewClient(),
			Arch:                      arch,
			Platform:                  hostPlatform,
			SquashFsCompressionConfig: constants.GetDefaultSquashfsCompressionOptions(),
			ImageExtractor:            imageextractor.OCIImageExtractor{},
			SquashFsNoCompression:     true,
			Install:                   &install.Install{},
			UkiMaxEntries:             constants.UkiMaxEntries,
		},
	}
	for _, o := range opts {
		o(c)
	}

	// delay runner creation after we have run over the options in case we use WithRunner
	if c.Runner == nil {
		c.Runner = &runner2.RealRunner{Logger: &c.Logger}
	}

	// Now check if the runner has a logger inside, otherwise point our logger into it
	// This can happen if we set the WithRunner option as that doesn't set a logger
	l := c.Runner.GetLogger()
	if &l == nil {
		c.Runner.SetLogger(&c.Logger)
	}

	// Delay the yip runner creation, so we set the proper logger instead of blindly setting it to the logger we create
	// at the start of NewConfig, as WithLogger can be passed on init, and that would result in 2 different logger
	// instances, on the config.Logger and the other on config.CloudInitRunner
	if c.CloudInitRunner == nil {
		c.CloudInitRunner = cloudinit.NewYipCloudInitRunner(c.Logger, c.Runner, vfs.OSFS)
	}

	if c.Mounter == nil {
		c.Mounter = mount.New(constants.MountBinary)
	}

	err = c.Sanitize()
	// This should never happen
	if err != nil {
		c.Logger.Warnf("Error sanitizing the config: %s", err)
	}

	return c
}

func contains(s []string, str string) bool {
	for _, v := range s {
		if v == str {
			return true
		}
	}
	return false
}

// CheckForUsers will check the config for any users and validate that at least we have 1 admin.
// Since Kairos 3.3.x we don't ship a default user with the system, so before a system with no specific users
// was relying in our default cloud-configs which created a kairos user ALWAYS (with SUDO!)
// But now we don't ship it anymore. So a user upgrading from 3.2.x to 3.3.x that created no users, will end up with a blocked
// system.
// So we need to see if they are setting a user in their config and if not refuse to continue
func (c Config) CheckForUsers() (err error) {
	// If nousers is enabled we do not check for the validity of the users and such
	// At this point, the config should be fully parsed and the yip stages ready

	// Check if the sentinel is present
	_, sentinel := c.Fs.Stat("/etc/kairos/.nousers")
	if sentinel == nil {
		c.Logger.Logger.Debug().Msg("Sentinel file found, skipping user check")
		return nil
	}
	if !c.Install.NoUsers {
		anyAdmin := false
		cc, _ := c.Collector.String()
		yamlConfig, err := yip.Load(cc, vfs.OSFS, nil, nil)
		if err != nil {
			return err
		}
		for _, stage := range yamlConfig.Stages {
			for _, x := range stage {
				if len(x.Users) > 0 {
					for _, user := range x.Users {
						if contains(user.Groups, "admin") || user.PrimaryGroup == "admin" {
							anyAdmin = true
							break
						}
					}
				}
			}

		}
		if !anyAdmin {
			return fmt.Errorf("No users found in any stage that are part of the 'admin' group.\n" +
				"In Kairos 3.3.x we no longer ship a default hardcoded user with the system configs and require users to provide their own user." +
				"Please provide at least 1 user that is part of the 'admin' group(for sudo) with your cloud configs." +
				"If you still want to continue without creating any users in the system, set 'install.nousers: true' to be in the config in order to allow a system with no users.")
		}
	}
	return err
}

// Sanitize checks the consistency of the struct, returns error
// if unsolvable inconsistencies are found
func (c *Config) Sanitize() error {
	// If no squashcompression is set, zero the compression parameters
	// By default on NewConfig the SquashFsCompressionConfig is set to the default values, and then override
	// on config unmarshall.
	if c.SquashFsNoCompression {
		c.SquashFsCompressionConfig = []string{}
	}
	if c.Arch != "" && c.Platform == nil {
		p, err := platform.NewPlatformFromArch(c.Arch)
		if err != nil {
			return err
		}
		c.Platform = p
	}

	if c.Platform == nil {
		p, err := platform.NewPlatformFromArch(runtime.GOARCH)
		if err != nil {
			return err
		}
		c.Platform = p
	}
	return nil
}

type GenericOptions func(a *Config)

func WithFs(fs sdkFs.KairosFS) func(r *Config) {
	return func(r *Config) {
		r.Fs = fs
	}
}

func WithLogger(logger logger.KairosLogger) func(r *Config) {
	return func(r *Config) {
		r.Logger = logger
	}
}

func WithSyscall(syscall sdkSyscall.Interface) func(r *Config) {
	return func(r *Config) {
		r.Syscall = syscall
	}
}

func WithMounter(mounter mount.Interface) func(r *Config) {
	return func(r *Config) {
		r.Mounter = mounter
	}
}

func WithRunner(runner runner.Runner) func(r *Config) {
	return func(r *Config) {
		r.Runner = runner
	}
}

func WithClient(client sdkHttp.Client) func(r *Config) {
	return func(r *Config) {
		r.Client = client
	}
}

func WithCloudInitRunner(ci cloudinitrunner.CloudInitRunner) func(r *Config) {
	return func(r *Config) {
		r.CloudInitRunner = ci
	}
}

func WithPlatform(plat string) func(r *Config) {
	return func(r *Config) {
		p, err := platform.ParsePlatform(plat)
		if err == nil {
			r.Platform = p
		}
	}
}

func WithImageExtractor(extractor sdkImages.ImageExtractor) func(r *Config) {
	return func(r *Config) {
		r.ImageExtractor = extractor
	}
}

const DefaultHeader = "#cloud-config"

func HasHeader(userdata, head string) (bool, string) {
	header := strings.SplitN(userdata, "\n", 2)[0]

	// Trim trailing whitespaces
	header = strings.TrimRightFunc(header, unicode.IsSpace)

	if head != "" {
		return head == header, header
	}
	return (header == DefaultHeader) || (header == "#kairos-config") || (header == "#node-config"), header
}

// HasConfigURL returns true if ConfigURL has been set and false if it's empty.
func (c Config) HasConfigURL() bool {
	return c.ConfigURL != ""
}

// FilterKeys is used to pass to any other pkg which might want to see which part of the config matches the Kairos config.
func FilterKeys(d []byte) ([]byte, error) {
	cmdLineFilter := Config{}
	err := yaml.Unmarshal(d, &cmdLineFilter)
	if err != nil {
		return []byte{}, err
	}

	out, err := yaml.Marshal(cmdLineFilter)
	if err != nil {
		return []byte{}, err
	}

	return out, nil
}

// ScanNoLogs is a wrapper around Scan that sets the logger to null
// Also sets the NoLogs option to true by default
func ScanNoLogs(opts ...collector.Option) (c *Config, err error) {
	log := logger.NewNullLogger()
	result := NewConfig(WithLogger(log))
	return scan(result, append(opts, collector.NoLogs)...)
}

// Scan is a wrapper around collector.Scan that sets the logger to the default Kairos logger
func Scan(opts ...collector.Option) (c *Config, err error) {
	result := NewConfig()
	return scan(result, opts...)
}

// scan is the internal function that does the actual scanning of the configs
func scan(result *Config, opts ...collector.Option) (c *Config, err error) {
	// Init new config with some default options
	o := &collector.Options{}
	if err := o.Apply(opts...); err != nil {
		return result, err
	}

	genericConfig, err := collector.Scan(o, FilterKeys)
	if err != nil {
		return result, err
	}

	result.Config.Collector = *genericConfig
	configStr, err := genericConfig.String()
	if err != nil {
		return result, err
	}

	err = yaml.Unmarshal([]byte(configStr), result)
	if err != nil {
		return result, err
	}

	kc, err := schema.NewConfigFromYAML(configStr, schema.RootSchema{})
	if err != nil {
		if !o.NoLogs && !o.StrictValidation {
			fmt.Printf("WARNING: %s\n", err.Error())
		}

		if o.StrictValidation {
			return result, fmt.Errorf("ERROR: %s", err.Error())
		}
	}

	if !kc.IsValid() {
		if !o.NoLogs && !o.StrictValidation {
			fmt.Printf("WARNING: %s\n", kc.ValidationError.Error())
		}

		if o.StrictValidation {
			return result, fmt.Errorf("ERROR: %s", kc.ValidationError.Error())
		}
	}

	// If we got debug enabled via cloud config, set it on viper so its available everywhere
	if result.Debug {
		viper.Set("debug", true)
	}
	// Config the logger
	if viper.GetBool("debug") {
		result.Logger.SetLevel("debug")
	}

	result.Logger.Logger.Info().Interface("version", version.GetVersion()).Msg("Kairos Agent")
	result.Logger.Logger.Debug().Interface("version", version.Get()).Msg("Kairos Agent")

	// Try to load the kairos version from the kairos-release file
	// Best effort, if it fails, we just ignore it
	f, err := result.Fs.Open("/etc/os-release")
	defer f.Close()
	osRelease, err := godotenv.Parse(f)
	if err == nil {
		v := osRelease["KAIROS_VERSION"]
		if v != "" {
			result.Logger.Logger.Info().Str("version", v).Msg("Kairos System")
		} else {
			// Fallback into os-release
			f, err = result.Fs.Open("/etc/os-release")
			defer f.Close()
			osRelease, err = godotenv.Parse(f)
			if err == nil {
				v = osRelease["KAIROS_VERSION"]
				if v != "" {
					result.Logger.Logger.Info().Str("version", v).Msg("Kairos System")
				}
			}
		}
	}

	// Log the boot mode
	r, err := state.NewRuntimeWithLogger(result.Logger.Logger)
	if err == nil {
		result.Logger.Logger.Info().Str("boot_mode", string(r.BootState)).Msg("Boot Mode")
	}

	// Detect if we are running on a UKI boot to also log it
	cmdline, err := result.Fs.ReadFile("/proc/cmdline")
	if err == nil {
		result.Logger.Logger.Info().Bool("result", state.DetectUKIboot(string(cmdline))).Msg("Boot in uki mode")
	}

	result.Logger.Debugf("Loaded config: %s", litter.Sdump(result))

	return result, nil
}

type Stage string

const (
	NetworkStage   Stage = "network"
	InitramfsStage Stage = "initramfs"
)

func (n Stage) String() string {
	return string(n)
}

func SaveCloudConfig(name Stage, yc yip.YipConfig) error {
	dnsYAML, err := yaml.Marshal(yc)
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join("usr", "local", "cloud-config", fmt.Sprintf("100_%s.yaml", name)), dnsYAML, 0700)
}

func FromString(s string, o interface{}) error {
	return yaml.Unmarshal([]byte(s), o)
}

func MergeYAML(objs ...interface{}) ([]byte, error) {
	content := [][]byte{}
	for _, o := range objs {
		dat, err := yaml.Marshal(o)
		if err != nil {
			return []byte{}, err
		}
		content = append(content, dat)
	}

	finalData := make(map[string]interface{})

	for _, c := range content {
		if err := yaml.Unmarshal(c, &finalData); err != nil {
			return []byte{}, err
		}
	}

	return yaml.Marshal(finalData)
}

func AddHeader(header, data string) string {
	return fmt.Sprintf("%s\n%s", header, data)
}

var errInvalidArch = fmt.Errorf("invalid arch")

func golangArchToArch(arch string) (string, error) {
	switch strings.ToLower(arch) {
	case constants.ArchAmd64:
		return constants.Archx86, nil
	case constants.ArchArm64:
		return constants.ArchArm64, nil
	default:
		return "", errInvalidArch
	}
}
