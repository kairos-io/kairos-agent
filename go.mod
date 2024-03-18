module github.com/kairos-io/kairos-agent/v2

go 1.19

// This versions require go1.20
replace (
	github.com/onsi/ginkgo/v2 v2.15.0 => github.com/onsi/ginkgo/v2 v2.12.1
	github.com/onsi/gomega v1.31.1 => github.com/onsi/gomega v1.28.0
)

require (
	github.com/Masterminds/semver/v3 v3.2.1
	github.com/Masterminds/sprig/v3 v3.2.3
	github.com/cavaliergopher/grab/v3 v3.0.1
	github.com/distribution/distribution v2.8.3+incompatible
	github.com/erikgeiser/promptkit v0.9.0
	github.com/google/go-containerregistry v0.17.0
	github.com/google/go-github/v40 v40.0.0
	github.com/hashicorp/go-multierror v1.1.1
	github.com/jaypipes/ghw v0.12.0
	github.com/joho/godotenv v1.5.1
	github.com/kairos-io/kairos-sdk v0.0.27-0.20240301083517-b3dfedfacbf4
	github.com/kairos-io/kcrypt v0.9.1
	github.com/labstack/echo/v4 v4.11.4
	github.com/mitchellh/mapstructure v1.5.0
	github.com/mudler/go-nodepair v0.0.0-20221223092639-ba399a66fdfb
	github.com/mudler/go-pluggable v0.0.0-20230126220627-7710299a0ae5
	github.com/mudler/go-processmanager v0.0.0-20230818213616-f204007f963c
	github.com/mudler/yip v1.5.0
	github.com/nxadm/tail v1.4.11
	github.com/onsi/ginkgo/v2 v2.15.0
	github.com/onsi/gomega v1.31.1
	github.com/pterm/pterm v0.12.65
	github.com/rs/zerolog v1.32.0
	github.com/sanity-io/litter v1.5.5
	github.com/sirupsen/logrus v1.9.3
	github.com/spf13/viper v1.16.0
	github.com/twpayne/go-vfs/v4 v4.3.0 // v5 requires go1.20
	github.com/urfave/cli/v2 v2.26.0
	golang.org/x/net v0.19.0
	golang.org/x/oauth2 v0.12.0
	golang.org/x/sys v0.17.0
	gopkg.in/yaml.v1 v1.0.0-20140924161607-9f9df34309c0
	gopkg.in/yaml.v2 v2.4.0
	gopkg.in/yaml.v3 v3.0.1
	k8s.io/mount-utils v0.27.4
)

require (
	atomicgo.dev/cursor v0.2.0 // indirect
	atomicgo.dev/keyboard v0.2.9 // indirect
	atomicgo.dev/schedule v0.0.2 // indirect
	dario.cat/mergo v1.0.0 // indirect
	github.com/Masterminds/goutils v1.1.1 // indirect
	github.com/Microsoft/go-winio v0.6.1 // indirect
	github.com/Microsoft/hcsshim v0.11.1 // indirect
	github.com/ProtonMail/go-crypto v0.0.0-20230828082145-3c4c8a2d2371 // indirect
	github.com/StackExchange/wmi v1.2.1 // indirect
	github.com/acomagu/bufpipe v1.0.3 // indirect
	github.com/anatol/devmapper.go v0.0.0-20220907161421-ba4de5fc0fd1 // indirect
	github.com/anatol/luks.go v0.0.0-20230125211543-ada2562d4206 // indirect
	github.com/atotto/clipboard v0.1.4 // indirect
	github.com/avast/retry-go v3.0.0+incompatible // indirect
	github.com/aybabtme/rgbterm v0.0.0-20170906152045-cc83f3b3ce59 // indirect
	github.com/aymanbagabas/go-osc52/v2 v2.0.1 // indirect
	github.com/cavaliergopher/grab v2.0.0+incompatible // indirect
	github.com/charmbracelet/bubbles v0.16.1 // indirect
	github.com/charmbracelet/bubbletea v0.24.2 // indirect
	github.com/charmbracelet/lipgloss v0.7.1 // indirect
	github.com/chuckpreslar/emission v0.0.0-20170206194824-a7ddd980baf9 // indirect
	github.com/cloudflare/circl v1.3.3 // indirect
	github.com/containerd/cgroups v1.1.0 // indirect
	github.com/containerd/console v1.0.4-0.20230313162750-1ae8d489ac81 // indirect
	github.com/containerd/containerd v1.7.7 // indirect
	github.com/containerd/continuity v0.4.2 // indirect
	github.com/containerd/log v0.1.0 // indirect
	github.com/containerd/stargz-snapshotter/estargz v0.14.3 // indirect
	github.com/cpuguy83/go-md2man/v2 v2.0.3 // indirect
	github.com/cyphar/filepath-securejoin v0.2.4 // indirect
	github.com/denisbrodbeck/machineid v1.0.1 // indirect
	github.com/dgryski/go-camellia v0.0.0-20191119043421-69a8a13fb23d // indirect
	github.com/disintegration/imaging v1.6.2 // indirect
	github.com/diskfs/go-diskfs v1.3.0 // indirect
	github.com/distribution/reference v0.5.0 // indirect
	github.com/docker/cli v24.0.0+incompatible // indirect
	github.com/docker/distribution v2.8.2+incompatible // indirect
	github.com/docker/docker v24.0.0+incompatible // indirect
	github.com/docker/docker-credential-helpers v0.7.0 // indirect
	github.com/docker/go-connections v0.4.0 // indirect
	github.com/docker/go-units v0.5.0 // indirect
	github.com/eliukblau/pixterm v1.3.1 // indirect
	github.com/emirpasic/gods v1.18.1 // indirect
	github.com/fsnotify/fsnotify v1.6.0 // indirect
	github.com/gen2brain/shm v0.0.0-20200228170931-49f9650110c5 // indirect
	github.com/ghodss/yaml v1.0.0 // indirect
	github.com/go-git/gcfg v1.5.1-0.20230307220236-3a3c6141e376 // indirect
	github.com/go-git/go-billy/v5 v5.5.0 // indirect
	github.com/go-git/go-git/v5 v5.11.0 // indirect
	github.com/go-logr/logr v1.3.0 // indirect
	github.com/go-ole/go-ole v1.2.6 // indirect
	github.com/go-task/slim-sprig v0.0.0-20230315185526-52ccab3ef572 // indirect
	github.com/gofrs/uuid v4.4.0+incompatible // indirect
	github.com/gogo/protobuf v1.3.2 // indirect
	github.com/golang/groupcache v0.0.0-20210331224755-41bb18bfe9da // indirect
	github.com/golang/protobuf v1.5.3 // indirect
	github.com/google/go-cmp v0.6.0 // indirect
	github.com/google/go-querystring v1.1.0 // indirect
	github.com/google/pprof v0.0.0-20230228050547-1710fef4ab10 // indirect
	github.com/google/shlex v0.0.0-20191202100458-e7afc7fbc510 // indirect
	github.com/google/uuid v1.3.0 // indirect
	github.com/gookit/color v1.5.3 // indirect
	github.com/hashicorp/errwrap v1.1.0 // indirect
	github.com/hashicorp/hcl v1.0.0 // indirect
	github.com/huandu/xstrings v1.3.3 // indirect
	github.com/imdario/mergo v0.3.15 // indirect
	github.com/inconshreveable/mousetrap v1.1.0 // indirect
	github.com/itchyny/gojq v0.12.13 // indirect
	github.com/itchyny/timefmt-go v0.1.5 // indirect
	github.com/jaypipes/pcidb v1.0.0 // indirect
	github.com/jbenet/go-context v0.0.0-20150711004518-d14ea06fba99 // indirect
	github.com/jezek/xgb v0.0.0-20210312150743-0e0f116e1240 // indirect
	github.com/kbinani/screenshot v0.0.0-20210720154843-7d3a670d8329 // indirect
	github.com/kendru/darwin/go/depgraph v0.0.0-20221105232959-877d6a81060c // indirect
	github.com/kevinburke/ssh_config v1.2.0 // indirect
	github.com/klauspost/compress v1.16.5 // indirect
	github.com/labstack/gommon v0.4.2 // indirect
	github.com/lithammer/fuzzysearch v1.1.8 // indirect
	github.com/lucasb-eyer/go-colorful v1.2.0 // indirect
	github.com/lufia/plan9stats v0.0.0-20211012122336-39d0f177ccd0 // indirect
	github.com/lxn/win v0.0.0-20210218163916-a377121e959e // indirect
	github.com/magiconair/properties v1.8.7 // indirect
	github.com/makiuchi-d/gozxing v0.1.1 // indirect
	github.com/mattn/go-colorable v0.1.13 // indirect
	github.com/mattn/go-isatty v0.0.20 // indirect
	github.com/mattn/go-localereader v0.0.1 // indirect
	github.com/mattn/go-runewidth v0.0.15 // indirect
	github.com/mitchellh/copystructure v1.2.0 // indirect
	github.com/mitchellh/go-homedir v1.1.0 // indirect
	github.com/mitchellh/reflectwalk v1.0.2 // indirect
	github.com/moby/moby v24.0.9+incompatible // indirect
	github.com/moby/sys/mountinfo v0.6.2 // indirect
	github.com/moby/sys/sequential v0.5.0 // indirect
	github.com/mudler/entities v0.0.0-20220905203055-68348bae0f49 // indirect
	github.com/muesli/ansi v0.0.0-20230316100256-276c6243b2f6 // indirect
	github.com/muesli/cancelreader v0.2.2 // indirect
	github.com/muesli/reflow v0.3.0 // indirect
	github.com/muesli/termenv v0.15.2 // indirect
	github.com/nfnt/resize v0.0.0-20180221191011-83c6a9932646 // indirect
	github.com/onsi/ginkgo v1.16.5 // indirect
	github.com/opencontainers/go-digest v1.0.0 // indirect
	github.com/opencontainers/image-spec v1.1.0-rc3 // indirect
	github.com/otiai10/copy v1.9.0 // indirect
	github.com/packethost/packngo v0.29.0 // indirect
	github.com/pelletier/go-toml/v2 v2.0.8 // indirect
	github.com/phayes/permbits v0.0.0-20190612203442-39d7c581d2ee // indirect
	github.com/pierrec/lz4 v2.6.1+incompatible // indirect
	github.com/pjbgf/sha1cd v0.3.0 // indirect
	github.com/pkg/errors v0.9.1 // indirect
	github.com/pkg/xattr v0.4.9 // indirect
	github.com/power-devops/perfstat v0.0.0-20210106213030-5aafc221ea8c // indirect
	github.com/qeesung/image2ascii v1.0.1 // indirect
	github.com/rancher-sandbox/linuxkit v1.0.2 // indirect
	github.com/rivo/uniseg v0.4.4 // indirect
	github.com/russross/blackfriday/v2 v2.1.0 // indirect
	github.com/samber/lo v1.37.0 // indirect
	github.com/santhosh-tekuri/jsonschema/v5 v5.3.1 // indirect
	github.com/satori/go.uuid v1.2.1-0.20181028125025-b2ce2384e17b // indirect
	github.com/sergi/go-diff v1.3.1 // indirect
	github.com/shirou/gopsutil/v3 v3.23.7 // indirect
	github.com/shoenig/go-m1cpu v0.1.6 // indirect
	github.com/shopspring/decimal v1.3.1 // indirect
	github.com/skeema/knownhosts v1.2.1 // indirect
	github.com/skip2/go-qrcode v0.0.0-20200617195104-da1b6568686e // indirect
	github.com/spectrocloud-labs/herd v0.4.2 // indirect
	github.com/spf13/afero v1.9.5 // indirect
	github.com/spf13/cast v1.5.1 // indirect
	github.com/spf13/cobra v1.8.0 // indirect
	github.com/spf13/jwalterweatherman v1.1.0 // indirect
	github.com/spf13/pflag v1.0.5 // indirect
	github.com/subosito/gotenv v1.4.2 // indirect
	github.com/swaggest/jsonschema-go v0.3.62 // indirect
	github.com/swaggest/refl v1.3.0 // indirect
	github.com/tklauser/go-sysconf v0.3.11 // indirect
	github.com/tklauser/numcpus v0.6.0 // indirect
	github.com/tredoe/osutil/v2 v2.0.0-rc.16 // indirect
	github.com/ulikunitz/xz v0.5.11 // indirect
	github.com/valyala/bytebufferpool v1.0.0 // indirect
	github.com/valyala/fasttemplate v1.2.2 // indirect
	github.com/vbatts/tar-split v0.11.3 // indirect
	github.com/vishvananda/netlink v1.2.1-beta.2 // indirect
	github.com/vishvananda/netns v0.0.4 // indirect
	github.com/vmware/vmw-guestinfo v0.0.0-20220317130741-510905f0efa3 // indirect
	github.com/wayneashleyberry/terminal-dimensions v1.1.0 // indirect
	github.com/willdonnelly/passwd v0.0.0-20141013001024-7935dab3074c // indirect
	github.com/xanzy/ssh-agent v0.3.3 // indirect
	github.com/xo/terminfo v0.0.0-20220910002029-abceb7e1c41e // indirect
	github.com/xrash/smetrics v0.0.0-20201216005158-039620a65673 // indirect
	github.com/yusufpapurcu/wmi v1.2.3 // indirect
	github.com/zcalusic/sysinfo v1.0.1 // indirect
	go.opencensus.io v0.24.0 // indirect
	golang.org/x/crypto v0.17.0 // indirect
	golang.org/x/exp v0.0.0-20230522175609-2e198f4a06a1 // indirect
	golang.org/x/image v0.0.0-20191206065243-da761ea9ff43 // indirect
	golang.org/x/mod v0.14.0 // indirect
	golang.org/x/sync v0.5.0 // indirect
	golang.org/x/term v0.15.0 // indirect
	golang.org/x/text v0.14.0 // indirect
	golang.org/x/tools v0.16.1 // indirect
	golang.org/x/xerrors v0.0.0-20220907171357-04be3eba64a2 // indirect
	google.golang.org/appengine v1.6.7 // indirect
	google.golang.org/genproto v0.0.0-20230410155749-daa745c078e1 // indirect
	google.golang.org/grpc v1.55.0 // indirect
	google.golang.org/protobuf v1.31.0 // indirect
	gopkg.in/djherbis/times.v1 v1.3.0 // indirect
	gopkg.in/ini.v1 v1.67.0 // indirect
	gopkg.in/tomb.v1 v1.0.0-20141024135613-dd632973f1e7 // indirect
	gopkg.in/warnings.v0 v0.1.2 // indirect
	howett.net/plist v1.0.0 // indirect
	k8s.io/apimachinery v0.26.2 // indirect
	k8s.io/klog/v2 v2.90.1 // indirect
	k8s.io/utils v0.0.0-20230220204549-a5ecb0141aa5 // indirect
	pault.ag/go/modprobe v0.1.2 // indirect
	pault.ag/go/topsort v0.1.1 // indirect
)
