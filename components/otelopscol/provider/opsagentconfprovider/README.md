# Ops Agent Configuration Provider

## Description

The Ops Agent configuration provider (`opsagentconfprovider`) is a `confmap.Provider` (`opsagentconf` scheme) that reads a Google Cloud Ops Agent configuration file (`config.yaml`), merges it with the built-in default configuration, and translates it into an OpenTelemetry Collector configuration.

## Usage

```bash
otelopscol --config=opsagentconf:/etc/google-cloud-ops-agent/config.yaml
```

If no path is provided (`--config=opsagentconf:`), the provider defaults to the platform-standard Ops Agent configuration path:
- Linux: `/etc/google-cloud-ops-agent/config.yaml`
- Windows: `C:\Program Files\Google\Cloud Operations\Ops Agent\config\config.yaml`
