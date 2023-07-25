package config

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"unicode"

	"github.com/kairos-io/kairos-agent/v2/pkg/cloudinit"
	"github.com/kairos-io/kairos-agent/v2/pkg/constants"
	"github.com/kairos-io/kairos-agent/v2/pkg/http"
	v1 "github.com/kairos-io/kairos-agent/v2/pkg/types/v1"
	"github.com/kairos-io/kairos-sdk/bundles"
	"github.com/kairos-io/kairos-sdk/collector"
	"github.com/kairos-io/kairos-sdk/schema"
	yip "github.com/mudler/yip/pkg/schema"
	"github.com/sanity-io/litter"
	"github.com/spf13/viper"
	"github.com/twpayne/go-vfs"
	"gopkg.in/yaml.v3"
	"k8s.io/mount-utils"
)

const (
	DefaultWebUIListenAddress = ":8080"
	FilePrefix                = "file://"
)

type Install struct {
	Auto                   bool              `yaml:"auto,omitempty"`
	Reboot                 bool              `yaml:"reboot,omitempty"`
	NoFormat               bool              `yaml:"no_format,omitempty"`
	Device                 string            `yaml:"device,omitempty"`
	Poweroff               bool              `yaml:"poweroff,omitempty"`
	GrubOptions            map[string]string `yaml:"grub_options,omitempty"`
	Bundles                Bundles           `yaml:"bundles,omitempty"`
	Encrypt                []string          `yaml:"encrypted_partitions,omitempty"`
	SkipEncryptCopyPlugins bool              `yaml:"skip_copy_kcrypt_plugin,omitempty"`
	Env                    []string          `yaml:"env,omitempty"`
	Image                  string            `yaml:"image,omitempty"`
	EphemeralMounts        []string          `yaml:"ephemeral_mounts,omitempty"`
	BindMounts             []string          `yaml:"bind_mounts,omitempty"`
}

func NewConfig(opts ...GenericOptions) *Config {
	log := v1.NewLogger()

	hostPlatform, err := v1.NewPlatformFromArch(runtime.GOARCH)
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
		Fs:                        vfs.OSFS,
		Logger:                    log,
		Syscall:                   &v1.RealSyscall{},
		Client:                    http.NewClient(),
		Arch:                      arch,
		Platform:                  hostPlatform,
		SquashFsCompressionConfig: constants.GetDefaultSquashfsCompressionOptions(),
		ImageExtractor:            v1.OCIImageExtractor{},
		SquashFsNoCompression:     true,
	}
	for _, o := range opts {
		o(c)
	}

	// delay runner creation after we have run over the options in case we use WithRunner
	if c.Runner == nil {
		c.Runner = &v1.RealRunner{Logger: c.Logger}
	}

	// Now check if the runner has a logger inside, otherwise point our logger into it
	// This can happen if we set the WithRunner option as that doesn't set a logger
	if c.Runner.GetLogger() == nil {
		c.Runner.SetLogger(c.Logger)
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

type Config struct {
	Install                   *Install `yaml:"install,omitempty"`
	collector.Config          `yaml:"-"`
	ConfigURL                 string              `yaml:"config_url,omitempty"`
	Options                   map[string]string   `yaml:"options,omitempty"`
	FailOnBundleErrors        bool                `yaml:"fail_on_bundles_errors,omitempty"`
	Bundles                   Bundles             `yaml:"bundles,omitempty"`
	GrubOptions               map[string]string   `yaml:"grub_options,omitempty"`
	Env                       []string            `yaml:"env,omitempty"`
	Debug                     bool                `yaml:"debug,omitempty" mapstructure:"debug"`
	Strict                    bool                `yaml:"strict,omitempty" mapstructure:"strict"`
	CloudInitPaths            []string            `yaml:"cloud-init-paths,omitempty" mapstructure:"cloud-init-paths"`
	EjectCD                   bool                `yaml:"eject-cd,omitempty" mapstructure:"eject-cd"`
	Logger                    v1.Logger           `yaml:"-"`
	Fs                        v1.FS               `yaml:"-"`
	Mounter                   mount.Interface     `yaml:"-"`
	Runner                    v1.Runner           `yaml:"-"`
	Syscall                   v1.SyscallInterface `yaml:"-"`
	CloudInitRunner           v1.CloudInitRunner  `yaml:"-"`
	ImageExtractor            v1.ImageExtractor   `yaml:"-"`
	Client                    v1.HTTPClient       `yaml:"-"`
	Platform                  *v1.Platform        `yaml:"platform,omitempty" mapstructure:"platform"`
	Cosign                    bool                `yaml:"cosign,omitempty" mapstructure:"cosign"`
	Verify                    bool                `yaml:"verify,omitempty" mapstructure:"verify"`
	CosignPubKey              string              `yaml:"cosign-key,omitempty" mapstructure:"cosign-key"`
	Arch                      string              `yaml:"arch,omitempty" mapstructure:"arch"`
	SquashFsCompressionConfig []string            `yaml:"squash-compression,omitempty" mapstructure:"squash-compression"`
	SquashFsNoCompression     bool                `yaml:"squash-no-compression,omitempty" mapstructure:"squash-no-compression"`
}

// WriteInstallState writes the state.yaml file to the given state and recovery paths
func (c Config) WriteInstallState(i *v1.InstallState, statePath, recoveryPath string) error {
	data, err := yaml.Marshal(i)
	if err != nil {
		return err
	}

	data = append([]byte("# Autogenerated file by elemental client, do not edit\n\n"), data...)

	err = c.Fs.WriteFile(statePath, data, constants.FilePerm)
	if err != nil {
		return err
	}

	err = c.Fs.WriteFile(recoveryPath, data, constants.FilePerm)
	if err != nil {
		return err
	}

	return nil
}

// LoadInstallState loads the state.yaml file and unmarshals it to an InstallState object
func (c Config) LoadInstallState() (*v1.InstallState, error) {
	installState := &v1.InstallState{}
	data, err := c.Fs.ReadFile(filepath.Join(constants.RunningStateDir, constants.InstallStateFile))
	if err != nil {
		return nil, err
	}
	err = yaml.Unmarshal(data, installState)
	if err != nil {
		return nil, err
	}
	return installState, nil
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
	if c.Arch != "" {
		p, err := v1.NewPlatformFromArch(c.Arch)
		if err != nil {
			return err
		}
		c.Platform = p
	}

	if c.Platform == nil {
		p, err := v1.NewPlatformFromArch(runtime.GOARCH)
		if err != nil {
			return err
		}
		c.Platform = p
	}
	return nil
}

type GenericOptions func(a *Config)

func WithFs(fs v1.FS) func(r *Config) {
	return func(r *Config) {
		r.Fs = fs
	}
}

func WithLogger(logger v1.Logger) func(r *Config) {
	return func(r *Config) {
		r.Logger = logger
	}
}

func WithSyscall(syscall v1.SyscallInterface) func(r *Config) {
	return func(r *Config) {
		r.Syscall = syscall
	}
}

func WithMounter(mounter mount.Interface) func(r *Config) {
	return func(r *Config) {
		r.Mounter = mounter
	}
}

func WithRunner(runner v1.Runner) func(r *Config) {
	return func(r *Config) {
		r.Runner = runner
	}
}

func WithClient(client v1.HTTPClient) func(r *Config) {
	return func(r *Config) {
		r.Client = client
	}
}

func WithCloudInitRunner(ci v1.CloudInitRunner) func(r *Config) {
	return func(r *Config) {
		r.CloudInitRunner = ci
	}
}

func WithPlatform(platform string) func(r *Config) {
	return func(r *Config) {
		p, err := v1.ParsePlatform(platform)
		if err == nil {
			r.Platform = p
		}
	}
}

func WithImageExtractor(extractor v1.ImageExtractor) func(r *Config) {
	return func(r *Config) {
		r.ImageExtractor = extractor
	}
}

type Bundles []Bundle

type Bundle struct {
	Repository string   `yaml:"repository,omitempty"`
	Rootfs     string   `yaml:"rootfs_path,omitempty"`
	DB         string   `yaml:"db_path,omitempty"`
	LocalFile  bool     `yaml:"local_file,omitempty"`
	Targets    []string `yaml:"targets,omitempty"`
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

func (b Bundles) Options() (res [][]bundles.BundleOption) {
	for _, bundle := range b {
		for _, t := range bundle.Targets {
			opts := []bundles.BundleOption{bundles.WithRepository(bundle.Repository), bundles.WithTarget(t)}
			if bundle.Rootfs != "" {
				opts = append(opts, bundles.WithRootFS(bundle.Rootfs))
			}
			if bundle.DB != "" {
				opts = append(opts, bundles.WithDBPath(bundle.DB))
			}
			if bundle.LocalFile {
				opts = append(opts, bundles.WithLocalFile(true))
			}
			res = append(res, opts)
		}
	}
	return
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

func Scan(opts ...collector.Option) (c *Config, err error) {
	// Init new config with some default options
	result := NewConfig()

	o := &collector.Options{}
	if err := o.Apply(opts...); err != nil {
		return result, err
	}

	genericConfig, err := collector.Scan(o, FilterKeys)
	if err != nil {
		return result, err

	}
	result.Config = *genericConfig
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
	configLogger(result.Logger, result.Fs)

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
