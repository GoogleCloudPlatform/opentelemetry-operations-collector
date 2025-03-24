module github.com/GoogleCloudPlatform/opentelemetry-operations-collector

go 1.24

require (
	github.com/GoogleCloudPlatform/opentelemetry-operations-collector/processor/agentmetricsprocessor v0.0.0-20250324171034-46253aa641a6
	github.com/GoogleCloudPlatform/opentelemetry-operations-collector/processor/casttosumprocessor v0.0.0-20250324171034-46253aa641a6
	github.com/GoogleCloudPlatform/opentelemetry-operations-collector/processor/modifyscopeprocessor v0.0.0-20250324171034-46253aa641a6
	github.com/GoogleCloudPlatform/opentelemetry-operations-collector/processor/normalizesumsprocessor v0.0.0-20250324171034-46253aa641a6
	github.com/GoogleCloudPlatform/opentelemetry-operations-collector/receiver/dcgmreceiver v0.0.0-20250324171034-46253aa641a6
	github.com/GoogleCloudPlatform/opentelemetry-operations-collector/receiver/mongodbreceiver v0.0.0-20250324171034-46253aa641a6
	github.com/GoogleCloudPlatform/opentelemetry-operations-collector/receiver/nvmlreceiver v0.0.0-20250324171034-46253aa641a6
	github.com/GoogleCloudPlatform/opentelemetry-operations-collector/receiver/varnishreceiver v0.0.0-20250324171034-46253aa641a6
	github.com/google/go-cmp v0.7.0
	github.com/open-telemetry/opentelemetry-collector-contrib/exporter/fileexporter v0.122.0
	github.com/open-telemetry/opentelemetry-collector-contrib/exporter/googlecloudexporter v0.122.0
	github.com/open-telemetry/opentelemetry-collector-contrib/exporter/googlemanagedprometheusexporter v0.122.0
	github.com/open-telemetry/opentelemetry-collector-contrib/processor/cumulativetodeltaprocessor v0.122.0
	github.com/open-telemetry/opentelemetry-collector-contrib/processor/deltatorateprocessor v0.122.0
	github.com/open-telemetry/opentelemetry-collector-contrib/processor/filterprocessor v0.122.0
	github.com/open-telemetry/opentelemetry-collector-contrib/processor/groupbyattrsprocessor v0.122.0
	github.com/open-telemetry/opentelemetry-collector-contrib/processor/metricstransformprocessor v0.122.0
	github.com/open-telemetry/opentelemetry-collector-contrib/processor/resourcedetectionprocessor v0.122.0
	github.com/open-telemetry/opentelemetry-collector-contrib/processor/resourceprocessor v0.122.0
	github.com/open-telemetry/opentelemetry-collector-contrib/processor/transformprocessor v0.122.0
	github.com/open-telemetry/opentelemetry-collector-contrib/receiver/activedirectorydsreceiver v0.122.0
	github.com/open-telemetry/opentelemetry-collector-contrib/receiver/aerospikereceiver v0.122.0
	github.com/open-telemetry/opentelemetry-collector-contrib/receiver/apachereceiver v0.122.0
	github.com/open-telemetry/opentelemetry-collector-contrib/receiver/couchdbreceiver v0.122.0
	github.com/open-telemetry/opentelemetry-collector-contrib/receiver/elasticsearchreceiver v0.122.0
	github.com/open-telemetry/opentelemetry-collector-contrib/receiver/filelogreceiver v0.122.0
	github.com/open-telemetry/opentelemetry-collector-contrib/receiver/flinkmetricsreceiver v0.122.0
	github.com/open-telemetry/opentelemetry-collector-contrib/receiver/hostmetricsreceiver v0.122.0
	github.com/open-telemetry/opentelemetry-collector-contrib/receiver/iisreceiver v0.122.0
	github.com/open-telemetry/opentelemetry-collector-contrib/receiver/jmxreceiver v0.122.0
	github.com/open-telemetry/opentelemetry-collector-contrib/receiver/journaldreceiver v0.122.0
	github.com/open-telemetry/opentelemetry-collector-contrib/receiver/memcachedreceiver v0.122.0
	github.com/open-telemetry/opentelemetry-collector-contrib/receiver/mysqlreceiver v0.122.0
	github.com/open-telemetry/opentelemetry-collector-contrib/receiver/nginxreceiver v0.122.0
	github.com/open-telemetry/opentelemetry-collector-contrib/receiver/otlpjsonfilereceiver v0.119.0
	github.com/open-telemetry/opentelemetry-collector-contrib/receiver/postgresqlreceiver v0.122.0
	github.com/open-telemetry/opentelemetry-collector-contrib/receiver/prometheusreceiver v0.122.0
	github.com/open-telemetry/opentelemetry-collector-contrib/receiver/rabbitmqreceiver v0.122.0
	github.com/open-telemetry/opentelemetry-collector-contrib/receiver/redisreceiver v0.122.0
	github.com/open-telemetry/opentelemetry-collector-contrib/receiver/saphanareceiver v0.122.0
	github.com/open-telemetry/opentelemetry-collector-contrib/receiver/sqlqueryreceiver v0.122.0
	github.com/open-telemetry/opentelemetry-collector-contrib/receiver/sqlserverreceiver v0.122.0
	github.com/open-telemetry/opentelemetry-collector-contrib/receiver/windowseventlogreceiver v0.122.0
	github.com/open-telemetry/opentelemetry-collector-contrib/receiver/windowsperfcountersreceiver v0.122.0
	github.com/open-telemetry/opentelemetry-collector-contrib/receiver/zookeeperreceiver v0.122.0
	go.opentelemetry.io/collector/exporter v0.122.1
	go.opentelemetry.io/collector/exporter/debugexporter v0.122.1
	go.opentelemetry.io/collector/exporter/otlpexporter v0.122.1
	go.opentelemetry.io/collector/exporter/otlphttpexporter v0.122.1
	go.opentelemetry.io/collector/extension v1.28.1
	go.opentelemetry.io/collector/extension/zpagesextension v0.122.1
	go.opentelemetry.io/collector/otelcol v0.122.1
	go.opentelemetry.io/collector/processor v0.122.1
	go.opentelemetry.io/collector/processor/batchprocessor v0.122.1
	go.opentelemetry.io/collector/processor/memorylimiterprocessor v0.122.1
	go.opentelemetry.io/collector/receiver v1.28.1
	go.opentelemetry.io/collector/receiver/otlpreceiver v0.122.1
	gopkg.in/yaml.v3 v3.0.1
)

require (
	cloud.google.com/go/auth v0.14.0 // indirect
	cloud.google.com/go/auth/oauth2adapt v0.2.7 // indirect
	filippo.io/edwards25519 v1.1.0 // indirect
	github.com/99designs/go-keychain v0.0.0-20191008050251-8e49817e8af4 // indirect
	github.com/99designs/keyring v1.2.2 // indirect
	github.com/Azure/azure-sdk-for-go/sdk/azcore v1.14.0 // indirect
	github.com/Azure/azure-sdk-for-go/sdk/azidentity v1.7.0 // indirect
	github.com/Azure/azure-sdk-for-go/sdk/internal v1.10.0 // indirect
	github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/compute/armcompute/v5 v5.7.0 // indirect
	github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/network/armnetwork/v4 v4.3.0 // indirect
	github.com/Azure/azure-sdk-for-go/sdk/storage/azblob v1.2.0 // indirect
	github.com/AzureAD/microsoft-authentication-library-for-go v1.2.2 // indirect
	github.com/BurntSushi/toml v1.4.0 // indirect
	github.com/Code-Hex/go-generics-cache v1.5.1 // indirect
	github.com/GoogleCloudPlatform/opentelemetry-operations-go/exporter/collector v0.51.0 // indirect
	github.com/GoogleCloudPlatform/opentelemetry-operations-go/exporter/collector/googlemanagedprometheus v0.51.0 // indirect
	github.com/JohnCGriffin/overflow v0.0.0-20211019200055-46fa312c352c // indirect
	github.com/Masterminds/semver v1.5.0 // indirect
	github.com/NVIDIA/go-dcgm v0.0.0-20240910155525-85ceb314ca65 // indirect
	github.com/NVIDIA/go-nvml v0.12.4-0 // indirect
	github.com/aerospike/aerospike-client-go/v7 v7.9.0 // indirect
	github.com/antchfx/xmlquery v1.4.4 // indirect
	github.com/antchfx/xpath v1.3.3 // indirect
	github.com/apache/arrow/go/v15 v15.0.0 // indirect
	github.com/asaskevich/govalidator v0.0.0-20230301143203-a9d515a09cc2 // indirect
	github.com/aws/aws-sdk-go-v2/config v1.29.9 // indirect
	github.com/aws/aws-sdk-go-v2/feature/ec2/imds v1.16.30 // indirect
	github.com/aws/aws-sdk-go-v2/internal/ini v1.8.3 // indirect
	github.com/aws/aws-sdk-go-v2/internal/v4a v1.3.5 // indirect
	github.com/aws/aws-sdk-go-v2/service/ec2 v1.209.0 // indirect
	github.com/aws/aws-sdk-go-v2/service/internal/checksum v1.3.7 // indirect
	github.com/aws/aws-sdk-go-v2/service/sso v1.25.1 // indirect
	github.com/aws/aws-sdk-go-v2/service/ssooidc v1.29.1 // indirect
	github.com/aws/aws-sdk-go-v2/service/sts v1.33.17 // indirect
	github.com/bboreham/go-loser v0.0.0-20230920113527-fcc2c21820a3 // indirect
	github.com/bits-and-blooms/bitset v1.13.0 // indirect
	github.com/bmatcuk/doublestar/v4 v4.8.1 // indirect
	github.com/cenkalti/backoff/v5 v5.0.2 // indirect
	github.com/coreos/go-systemd/v22 v22.5.0 // indirect
	github.com/danieljoos/wincred v1.1.2 // indirect
	github.com/dgryski/go-rendezvous v0.0.0-20200823014737-9f7001d12a5f // indirect
	github.com/distribution/reference v0.6.0 // indirect
	github.com/dvsekhvalnov/jose2go v1.6.0 // indirect
	github.com/ebitengine/purego v0.8.2 // indirect
	github.com/edsrzf/mmap-go v1.1.0 // indirect
	github.com/elastic/go-grok v0.3.1 // indirect
	github.com/elastic/lunes v0.1.0 // indirect
	github.com/envoyproxy/go-control-plane/envoy v1.32.4 // indirect
	github.com/expr-lang/expr v1.17.0 // indirect
	github.com/facette/natsort v0.0.0-20181210072756-2cd4dd1e2dcb // indirect
	github.com/fxamacker/cbor/v2 v2.7.0 // indirect
	github.com/go-openapi/analysis v0.22.2 // indirect
	github.com/go-openapi/errors v0.22.0 // indirect
	github.com/go-openapi/loads v0.21.5 // indirect
	github.com/go-openapi/spec v0.20.14 // indirect
	github.com/go-openapi/strfmt v0.23.0 // indirect
	github.com/go-openapi/validate v0.23.0 // indirect
	github.com/go-viper/mapstructure/v2 v2.2.1 // indirect
	github.com/goccy/go-json v0.10.5 // indirect
	github.com/godbus/dbus v0.0.0-20190726142602-4481cbc300e2 // indirect
	github.com/gogo/protobuf v1.3.2 // indirect
	github.com/golang-jwt/jwt/v5 v5.2.1 // indirect
	github.com/google/gnostic-models v0.6.8 // indirect
	github.com/google/s2a-go v0.1.9 // indirect
	github.com/grpc-ecosystem/grpc-gateway/v2 v2.26.1 // indirect
	github.com/gsterjov/go-libsecret v0.0.0-20161001094733-a6f4afe4910c // indirect
	github.com/hashicorp/errwrap v1.1.0 // indirect
	github.com/hashicorp/go-multierror v1.1.1 // indirect
	github.com/hashicorp/go-uuid v1.0.3 // indirect
	github.com/hashicorp/golang-lru/v2 v2.0.7 // indirect
	github.com/hetznercloud/hcloud-go/v2 v2.13.1 // indirect
	github.com/inconshreveable/mousetrap v1.1.0 // indirect
	github.com/jcmturner/aescts/v2 v2.0.0 // indirect
	github.com/jcmturner/dnsutils/v2 v2.0.0 // indirect
	github.com/jcmturner/gofork v1.7.6 // indirect
	github.com/jcmturner/goidentity/v6 v6.0.1 // indirect
	github.com/jcmturner/gokrb5/v8 v8.4.4 // indirect
	github.com/jcmturner/rpc/v2 v2.0.3 // indirect
	github.com/jonboulle/clockwork v0.5.0 // indirect
	github.com/json-iterator/go v1.1.12 // indirect
	github.com/julienschmidt/httprouter v1.3.0 // indirect
	github.com/klauspost/cpuid/v2 v2.2.5 // indirect
	github.com/knadh/koanf/maps v0.1.1 // indirect
	github.com/knadh/koanf/providers/confmap v0.1.0 // indirect
	github.com/knadh/koanf/v2 v2.1.2 // indirect
	github.com/kylelemons/godebug v1.1.0 // indirect
	github.com/leodido/go-syslog/v4 v4.2.0 // indirect
	github.com/leodido/ragel-machinery v0.0.0-20190525184631-5f46317e436b // indirect
	github.com/magefile/mage v1.15.0 // indirect
	github.com/mdlayher/socket v0.4.1 // indirect
	github.com/mdlayher/vsock v1.2.1 // indirect
	github.com/microsoft/go-mssqldb v1.8.0 // indirect
	github.com/mitchellh/go-homedir v1.1.0 // indirect
	github.com/mitchellh/mapstructure v1.5.1-0.20231216201459-8508981c8b6c // indirect
	github.com/moby/docker-image-spec v1.3.1 // indirect
	github.com/montanaflynn/stats v0.7.1 // indirect
	github.com/mostynb/go-grpc-compression v1.2.3 // indirect
	github.com/mtibben/percent v0.2.1 // indirect
	github.com/munnerz/goautoneg v0.0.0-20191010083416-a7dc8b61c822 // indirect
	github.com/mwitkow/go-conntrack v0.0.0-20190716064945-2f068394615f // indirect
	github.com/nginxinc/nginx-prometheus-exporter v0.11.0 // indirect
	github.com/oklog/ulid v1.3.1 // indirect
	github.com/open-telemetry/opentelemetry-collector-contrib/internal/aws/ecsutil v0.122.0 // indirect
	github.com/open-telemetry/opentelemetry-collector-contrib/internal/common v0.122.0 // indirect
	github.com/open-telemetry/opentelemetry-collector-contrib/internal/coreinternal v0.122.0 // indirect
	github.com/open-telemetry/opentelemetry-collector-contrib/internal/filter v0.122.0 // indirect
	github.com/open-telemetry/opentelemetry-collector-contrib/internal/k8sconfig v0.122.0 // indirect
	github.com/open-telemetry/opentelemetry-collector-contrib/internal/metadataproviders v0.122.0 // indirect
	github.com/open-telemetry/opentelemetry-collector-contrib/internal/pdatautil v0.122.0 // indirect
	github.com/open-telemetry/opentelemetry-collector-contrib/internal/sharedcomponent v0.122.0 // indirect
	github.com/open-telemetry/opentelemetry-collector-contrib/internal/sqlquery v0.122.0 // indirect
	github.com/open-telemetry/opentelemetry-collector-contrib/pkg/experimentalmetricmetadata v0.122.0 // indirect
	github.com/open-telemetry/opentelemetry-collector-contrib/pkg/pdatautil v0.122.0 // indirect
	github.com/open-telemetry/opentelemetry-collector-contrib/pkg/stanza v0.122.0 // indirect
	github.com/open-telemetry/opentelemetry-collector-contrib/pkg/translator/prometheus v0.122.0 // indirect
	github.com/open-telemetry/opentelemetry-collector-contrib/pkg/winperfcounters v0.122.0 // indirect
	github.com/open-telemetry/opentelemetry-collector-contrib/scraper/zookeeperscraper v0.122.0 // indirect
	github.com/opencontainers/go-digest v1.0.0 // indirect
	github.com/opencontainers/image-spec v1.1.0 // indirect
	github.com/openshift/api v3.9.0+incompatible // indirect
	github.com/openshift/client-go v0.0.0-20210521082421-73d9475a9142 // indirect
	github.com/ovh/go-ovh v1.6.0 // indirect
	github.com/pierrec/lz4/v4 v4.1.22 // indirect
	github.com/pkg/browser v0.0.0-20240102092130-5ac0b6a4141c // indirect
	github.com/pkg/errors v0.9.1 // indirect
	github.com/planetscale/vtprotobuf v0.6.1-0.20240319094008-0393e58bdf10 // indirect
	github.com/pmezard/go-difflib v1.0.1-0.20181226105442-5d4384ee4fb2 // indirect
	github.com/power-devops/perfstat v0.0.0-20210106213030-5aafc221ea8c // indirect
	github.com/prometheus-community/windows_exporter v0.27.2 // indirect
	github.com/prometheus/alertmanager v0.27.0 // indirect
	github.com/prometheus/client_golang v1.21.1 // indirect
	github.com/prometheus/client_model v0.6.1 // indirect
	github.com/prometheus/common v0.62.0 // indirect
	github.com/prometheus/common/assets v0.2.0 // indirect
	github.com/prometheus/common/sigv4 v0.1.0 // indirect
	github.com/prometheus/exporter-toolkit v0.13.0 // indirect
	github.com/prometheus/procfs v0.15.1 // indirect
	github.com/prometheus/prometheus v0.300.1 // indirect
	github.com/redis/go-redis/v9 v9.7.1 // indirect
	github.com/rs/cors v1.11.1 // indirect
	github.com/scaleway/scaleway-sdk-go v1.0.0-beta.30 // indirect
	github.com/shirou/gopsutil/v3 v3.24.5 // indirect
	github.com/shirou/gopsutil/v4 v4.25.2 // indirect
	github.com/shoenig/go-m1cpu v0.1.6 // indirect
	github.com/shurcooL/httpfs v0.0.0-20230704072500-f1e31cf0ba5c // indirect
	github.com/sijms/go-ora/v2 v2.8.24 // indirect
	github.com/sirupsen/logrus v1.9.3 // indirect
	github.com/snowflakedb/gosnowflake v1.12.0 // indirect
	github.com/stretchr/objx v0.5.2 // indirect
	github.com/stretchr/testify v1.10.0 // indirect
	github.com/thda/tds v0.1.7 // indirect
	github.com/tidwall/gjson v1.18.0 // indirect
	github.com/tidwall/match v1.1.1 // indirect
	github.com/tidwall/pretty v1.2.1 // indirect
	github.com/tidwall/tinylru v1.2.1 // indirect
	github.com/tidwall/wal v1.1.8 // indirect
	github.com/tilinna/clock v1.1.0 // indirect
	github.com/tklauser/go-sysconf v0.3.12 // indirect
	github.com/tklauser/numcpus v0.6.1 // indirect
	github.com/twmb/murmur3 v1.1.8 // indirect
	github.com/ua-parser/uap-go v0.0.0-20240611065828-3a4781585db6 // indirect
	github.com/valyala/fastjson v1.6.4 // indirect
	github.com/vultr/govultr/v2 v2.17.2 // indirect
	github.com/x448/float16 v0.8.4 // indirect
	github.com/xdg-go/pbkdf2 v1.0.0 // indirect
	github.com/xdg-go/scram v1.1.2 // indirect
	github.com/xdg-go/stringprep v1.0.4 // indirect
	github.com/youmark/pkcs8 v0.0.0-20240726163527-a2c0da244d78 // indirect
	github.com/yuin/gopher-lua v1.1.1 // indirect
	github.com/yusufpapurcu/wmi v1.2.4 // indirect
	github.com/zeebo/xxh3 v1.0.2 // indirect
	go.mongodb.org/mongo-driver v1.17.1 // indirect
	go.opentelemetry.io/auto/sdk v1.1.0 // indirect
	go.opentelemetry.io/collector v0.122.1 // indirect
	go.opentelemetry.io/collector/client v1.28.1 // indirect
	go.opentelemetry.io/collector/component/componentstatus v0.122.1 // indirect
	go.opentelemetry.io/collector/component/componenttest v0.122.1 // indirect
	go.opentelemetry.io/collector/config/configauth v0.122.1 // indirect
	go.opentelemetry.io/collector/config/configcompression v1.28.1 // indirect
	go.opentelemetry.io/collector/config/configgrpc v0.122.1 // indirect
	go.opentelemetry.io/collector/config/confighttp v0.122.1 // indirect
	go.opentelemetry.io/collector/config/confignet v1.28.1 // indirect
	go.opentelemetry.io/collector/config/configopaque v1.28.1 // indirect
	go.opentelemetry.io/collector/config/configretry v1.28.1 // indirect
	go.opentelemetry.io/collector/config/configtelemetry v0.122.1 // indirect
	go.opentelemetry.io/collector/config/configtls v1.28.1 // indirect
	go.opentelemetry.io/collector/confmap/xconfmap v0.122.1 // indirect
	go.opentelemetry.io/collector/connector v0.122.1 // indirect
	go.opentelemetry.io/collector/connector/connectortest v0.122.1 // indirect
	go.opentelemetry.io/collector/connector/xconnector v0.122.1 // indirect
	go.opentelemetry.io/collector/consumer v1.28.1 // indirect
	go.opentelemetry.io/collector/consumer/consumererror v0.122.1 // indirect
	go.opentelemetry.io/collector/consumer/consumererror/xconsumererror v0.122.1 // indirect
	go.opentelemetry.io/collector/consumer/consumertest v0.122.1 // indirect
	go.opentelemetry.io/collector/consumer/xconsumer v0.122.1 // indirect
	go.opentelemetry.io/collector/exporter/exporterhelper/xexporterhelper v0.122.1 // indirect
	go.opentelemetry.io/collector/exporter/exportertest v0.122.1 // indirect
	go.opentelemetry.io/collector/exporter/xexporter v0.122.1 // indirect
	go.opentelemetry.io/collector/extension/extensionauth v0.122.1 // indirect
	go.opentelemetry.io/collector/extension/extensioncapabilities v0.122.1 // indirect
	go.opentelemetry.io/collector/extension/extensiontest v0.122.1 // indirect
	go.opentelemetry.io/collector/extension/xextension v0.122.1 // indirect
	go.opentelemetry.io/collector/featuregate v1.28.1 // indirect
	go.opentelemetry.io/collector/internal/fanoutconsumer v0.122.1 // indirect
	go.opentelemetry.io/collector/internal/memorylimiter v0.122.1 // indirect
	go.opentelemetry.io/collector/internal/sharedcomponent v0.122.1 // indirect
	go.opentelemetry.io/collector/internal/telemetry v0.122.1 // indirect
	go.opentelemetry.io/collector/pdata/pprofile v0.122.1 // indirect
	go.opentelemetry.io/collector/pdata/testdata v0.122.1 // indirect
	go.opentelemetry.io/collector/pipeline v0.122.1 // indirect
	go.opentelemetry.io/collector/pipeline/xpipeline v0.122.1 // indirect
	go.opentelemetry.io/collector/processor/processorhelper/xprocessorhelper v0.122.1 // indirect
	go.opentelemetry.io/collector/processor/processortest v0.122.1 // indirect
	go.opentelemetry.io/collector/processor/xprocessor v0.122.1 // indirect
	go.opentelemetry.io/collector/receiver/receiverhelper v0.122.1 // indirect
	go.opentelemetry.io/collector/receiver/receivertest v0.122.1 // indirect
	go.opentelemetry.io/collector/receiver/xreceiver v0.122.1 // indirect
	go.opentelemetry.io/collector/scraper v0.122.0 // indirect
	go.opentelemetry.io/collector/scraper/scraperhelper v0.122.0 // indirect
	go.opentelemetry.io/collector/semconv v0.122.1 // indirect
	go.opentelemetry.io/collector/service v0.122.1 // indirect
	go.opentelemetry.io/collector/service/hostcapabilities v0.122.1 // indirect
	go.opentelemetry.io/contrib/bridges/otelzap v0.10.0 // indirect
	go.opentelemetry.io/contrib/instrumentation/google.golang.org/grpc/otelgrpc v0.60.0 // indirect
	go.opentelemetry.io/contrib/instrumentation/net/http/httptrace/otelhttptrace v0.56.0 // indirect
	go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp v0.60.0 // indirect
	go.opentelemetry.io/contrib/otelconf v0.15.0 // indirect
	go.opentelemetry.io/contrib/propagators/b3 v1.35.0 // indirect
	go.opentelemetry.io/contrib/zpages v0.60.0 // indirect
	go.opentelemetry.io/otel/exporters/otlp/otlplog/otlploggrpc v0.11.0 // indirect
	go.opentelemetry.io/otel/exporters/otlp/otlplog/otlploghttp v0.11.0 // indirect
	go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetricgrpc v1.35.0 // indirect
	go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetrichttp v1.35.0 // indirect
	go.opentelemetry.io/otel/exporters/otlp/otlptrace v1.35.0 // indirect
	go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc v1.35.0 // indirect
	go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp v1.35.0 // indirect
	go.opentelemetry.io/otel/exporters/prometheus v0.57.0 // indirect
	go.opentelemetry.io/otel/exporters/stdout/stdoutlog v0.11.0 // indirect
	go.opentelemetry.io/otel/exporters/stdout/stdoutmetric v1.35.0 // indirect
	go.opentelemetry.io/otel/exporters/stdout/stdouttrace v1.35.0 // indirect
	go.opentelemetry.io/otel/log v0.11.0 // indirect
	go.opentelemetry.io/otel/sdk v1.35.0 // indirect
	go.opentelemetry.io/otel/sdk/log v0.11.0 // indirect
	go.opentelemetry.io/otel/sdk/metric v1.35.0 // indirect
	go.opentelemetry.io/proto/otlp v1.5.0 // indirect
	go.uber.org/atomic v1.11.0 // indirect
	go.uber.org/goleak v1.3.0 // indirect
	go.uber.org/zap/exp v0.3.0 // indirect
	golang.org/x/crypto v0.36.0 // indirect
	golang.org/x/exp v0.0.0-20250106191152-7588d65b2ba8 // indirect
	golang.org/x/mod v0.22.0 // indirect
	golang.org/x/oauth2 v0.26.0 // indirect
	golang.org/x/sync v0.12.0 // indirect
	golang.org/x/term v0.30.0 // indirect
	golang.org/x/time v0.9.0 // indirect
	golang.org/x/tools v0.29.0 // indirect
	golang.org/x/xerrors v0.0.0-20220907171357-04be3eba64a2 // indirect
	gonum.org/v1/gonum v0.15.1 // indirect
	google.golang.org/api v0.216.0 // indirect
	google.golang.org/genproto v0.0.0-20250106144421-5f5ef82da422 // indirect
	google.golang.org/genproto/googleapis/api v0.0.0-20250218202821-56aae31c358a // indirect
	google.golang.org/genproto/googleapis/rpc v0.0.0-20250303144028-a0af3efb3deb // indirect
	gopkg.in/evanphx/json-patch.v4 v4.12.0 // indirect
	gopkg.in/inf.v0 v0.9.1 // indirect
	gopkg.in/ini.v1 v1.67.0 // indirect
	gopkg.in/natefinch/lumberjack.v2 v2.2.1 // indirect
	k8s.io/api v0.32.2 // indirect
	k8s.io/apimachinery v0.32.2 // indirect
	k8s.io/client-go v0.32.2 // indirect
	k8s.io/kube-openapi v0.0.0-20241105132330-32ad38e42d3f // indirect
	k8s.io/utils v0.0.0-20241104100929-3ea5e8cea738 // indirect
	sigs.k8s.io/json v0.0.0-20241010143419-9aa6b5e7a4b3 // indirect
	sigs.k8s.io/structured-merge-diff/v4 v4.4.2 // indirect
	sigs.k8s.io/yaml v1.4.0 // indirect
)

require (
	cloud.google.com/go/compute/metadata v0.6.0 // indirect
	cloud.google.com/go/longrunning v0.6.4 // indirect
	github.com/iancoleman/strcase v0.3.0 // indirect
	github.com/open-telemetry/opentelemetry-collector-contrib/pkg/ottl v0.122.0 // indirect; indir6.0
	gopkg.in/yaml.v2 v2.4.0 // indirect
	k8s.io/klog/v2 v2.130.1 // indirect
)

require (
	cloud.google.com/go v0.118.0 // indirect
	cloud.google.com/go/logging v1.13.0 // indirect
	cloud.google.com/go/monitoring v1.22.1 // indirect
	cloud.google.com/go/trace v1.11.3 // indirect
	github.com/GoogleCloudPlatform/opentelemetry-operations-go/detectors/gcp v1.27.0 // indirect
	github.com/GoogleCloudPlatform/opentelemetry-operations-go/exporter/trace v1.27.0 // indirect
	github.com/GoogleCloudPlatform/opentelemetry-operations-go/internal/resourcemapping v0.51.0 // indirect
	github.com/Microsoft/go-winio v0.6.2 // indirect
	github.com/SAP/go-hdb v1.13.4 // indirect
	github.com/Showmax/go-fqdn v1.0.0 // indirect
	github.com/alecthomas/participle/v2 v2.1.1 // indirect
	github.com/alecthomas/units v0.0.0-20240927000941-0f3dac36c52b // indirect
	github.com/armon/go-metrics v0.4.1 // indirect
	github.com/aws/aws-sdk-go v1.55.6 // indirect
	github.com/aws/aws-sdk-go-v2 v1.36.3 // indirect
	github.com/aws/aws-sdk-go-v2/aws/protocol/eventstream v1.6.2 // indirect
	github.com/aws/aws-sdk-go-v2/credentials v1.17.62 // indirect
	github.com/aws/aws-sdk-go-v2/feature/s3/manager v1.16.15 // indirect
	github.com/aws/aws-sdk-go-v2/internal/configsources v1.3.34 // indirect
	github.com/aws/aws-sdk-go-v2/internal/endpoints/v2 v2.6.34 // indirect
	github.com/aws/aws-sdk-go-v2/service/internal/accept-encoding v1.12.3 // indirect
	github.com/aws/aws-sdk-go-v2/service/internal/presigned-url v1.12.15 // indirect
	github.com/aws/aws-sdk-go-v2/service/internal/s3shared v1.17.5 // indirect
	github.com/aws/aws-sdk-go-v2/service/s3 v1.53.1 // indirect
	github.com/aws/smithy-go v1.22.2 // indirect
	github.com/beorn7/perks v1.0.1 // indirect
	github.com/cenkalti/backoff/v4 v4.3.0 // indirect
	github.com/cespare/xxhash/v2 v2.3.0 // indirect
	github.com/cncf/xds/go v0.0.0-20241223141626-cff3c89139a3 // indirect
	github.com/davecgh/go-spew v1.1.2-0.20180830191138-d8f796af33cc // indirect
	github.com/dennwc/varint v1.0.0 // indirect
	github.com/digitalocean/godo v1.126.0 // indirect
	github.com/docker/docker v27.5.1+incompatible // indirect
	github.com/docker/go-connections v0.5.0 // indirect
	github.com/docker/go-units v0.5.0 // indirect
	github.com/emicklei/go-restful/v3 v3.11.0 // indirect
	github.com/envoyproxy/protoc-gen-validate v1.2.1 // indirect
	github.com/fatih/color v1.16.0 // indirect
	github.com/felixge/httpsnoop v1.0.4 // indirect
	github.com/fsnotify/fsnotify v1.8.0 // indirect
	github.com/gabriel-vasile/mimetype v1.4.2 // indirect
	github.com/go-kit/log v0.2.1 // indirect
	github.com/go-logfmt/logfmt v0.6.0 // indirect
	github.com/go-logr/logr v1.4.2 // indirect
	github.com/go-logr/stdr v1.2.2 // indirect
	github.com/go-ole/go-ole v1.3.0 // indirect
	github.com/go-openapi/jsonpointer v0.21.0 // indirect
	github.com/go-openapi/jsonreference v0.20.4 // indirect
	github.com/go-openapi/swag v0.23.0 // indirect
	github.com/go-resty/resty/v2 v2.15.3 // indirect
	github.com/go-sql-driver/mysql v1.9.0 // indirect
	github.com/go-zookeeper/zk v1.0.4 // indirect
	github.com/gobwas/glob v0.2.3 // indirect
	github.com/golang-sql/civil v0.0.0-20220223132316-b832511892a9 // indirect
	github.com/golang-sql/sqlexp v0.1.0 // indirect
	github.com/golang/groupcache v0.0.0-20210331224755-41bb18bfe9da // indirect
	github.com/golang/protobuf v1.5.4 // indirect
	github.com/golang/snappy v1.0.0 // indirect
	github.com/google/flatbuffers v23.5.26+incompatible // indirect
	github.com/google/go-querystring v1.1.0 // indirect
	github.com/google/gofuzz v1.2.0 // indirect
	github.com/google/uuid v1.6.0 // indirect
	github.com/googleapis/enterprise-certificate-proxy v0.3.4 // indirect
	github.com/googleapis/gax-go/v2 v2.14.1 // indirect
	github.com/gophercloud/gophercloud v1.14.1 // indirect
	github.com/gorilla/websocket v1.5.0 // indirect
	github.com/grafana/regexp v0.0.0-20240518133315-a468a5bfb3bc // indirect
	github.com/grobie/gomemcache v0.0.0-20230213081705-239240bbc445 // indirect
	github.com/hashicorp/consul/api v1.31.2 // indirect
	github.com/hashicorp/cronexpr v1.1.2 // indirect
	github.com/hashicorp/go-cleanhttp v0.5.2 // indirect
	github.com/hashicorp/go-hclog v1.6.3 // indirect
	github.com/hashicorp/go-immutable-radix v1.3.1 // indirect
	github.com/hashicorp/go-retryablehttp v0.7.7 // indirect
	github.com/hashicorp/go-rootcerts v1.0.2 // indirect
	github.com/hashicorp/go-version v1.7.0 // indirect
	github.com/hashicorp/golang-lru v1.0.2 // indirect
	github.com/hashicorp/nomad/api v0.0.0-20241218080744-e3ac00f30eec // indirect
	github.com/hashicorp/serf v0.10.1 // indirect
	github.com/ionos-cloud/sdk-go/v6 v6.2.1 // indirect
	github.com/jmespath/go-jmespath v0.4.0 // indirect
	github.com/josharian/intern v1.0.0 // indirect
	github.com/jpillora/backoff v1.0.0 // indirect
	github.com/klauspost/compress v1.18.0 // indirect
	github.com/kolo/xmlrpc v0.0.0-20220921171641-a4b6fa1dd06b // indirect
	github.com/lib/pq v1.10.9 // indirect
	github.com/linode/linodego v1.41.0 // indirect
	github.com/lufia/plan9stats v0.0.0-20211012122336-39d0f177ccd0 // indirect
	github.com/mailru/easyjson v0.7.7 // indirect
	github.com/mattn/go-colorable v0.1.13 // indirect
	github.com/mattn/go-isatty v0.0.20 // indirect
	github.com/miekg/dns v1.1.62 // indirect
	github.com/mitchellh/copystructure v1.2.0 // indirect
	github.com/mitchellh/reflectwalk v1.0.2 // indirect
	github.com/modern-go/concurrent v0.0.0-20180306012644-bacd9c7ef1dd // indirect
	github.com/modern-go/reflect2 v1.0.2 // indirect
	github.com/spf13/cobra v1.9.1 // indirect
	github.com/spf13/pflag v1.0.6 // indirect
	go.opentelemetry.io/collector/component v1.28.1 // indirect
	go.opentelemetry.io/collector/confmap v1.28.1 // indirect
	go.opentelemetry.io/collector/filter v0.122.0 // indirect
	go.opentelemetry.io/collector/pdata v1.28.1 // indirect
	go.opentelemetry.io/otel v1.35.0 // indirect
	go.opentelemetry.io/otel/metric v1.35.0 // indirect
	go.opentelemetry.io/otel/trace v1.35.0 // indirect
	go.uber.org/multierr v1.11.0
	go.uber.org/zap v1.27.0 // indirect
	golang.org/x/net v0.37.0 // indirect
	golang.org/x/sys v0.31.0 // indirect
	golang.org/x/text v0.23.0 // indirect
	google.golang.org/grpc v1.71.0 // indirect
	google.golang.org/protobuf v1.36.5 // indirect
)
