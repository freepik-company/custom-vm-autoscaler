# Custom VM Autoscaler

![GitHub go.mod Go version (subdirectory of monorepo)](https://img.shields.io/github/go-mod/go-version/freepik-company/custom-vm-autoscaler)
![GitHub](https://img.shields.io/github/license/freepik-company/custom-vm-autoscaler)

It is a CLI that manages the scaling of virtual machines when certain conditions are met and executes some tasks prior to deleting the virtual machine in question.

## Motivation

In several use cases, solutions are deployed directly on virtual machines without using solutions like Kubernetes, and there is a need to scale and unscale these virtual machines just as one would with Kubernetes nodes or pods.

However, the solutions available on the market are quite limited in terms of checks, script executions, etc. This is where custom-vm-autoscaler comes in.

This solution is responsible for checking metrics from any supported market solution (Prometheus, Mimir, etc.) and, depending on whether a condition is met, creates or destroys virtual machines. Before destroying a virtual machine, X operations are performed (currently, for example, it fully drains the node with Elasticsearch).

This solution is open to any compatibility, and we encourage the community to develop them!

## Flags

As every configuration parameter can be defined in the config file, there are only few flags that can be defined.
They are described in the following table:

| Name          | Description                        |      Default      | Example                      |
|:--------------|:-----------------------------------|:-----------------:|:-----------------------------|
| `--config`    | Define the path to the config file | `autoscaler.yaml` | `--config ./autoscaler.yaml` |

## Environment variables

Some parameters can be defined not only by fixing them into the configuration file, but setting them as environment
variables. This is mostly used by credentials as a safer way to work with containers:

> Environment variables take precedence over configuration

| Name                      | Description                                                          | Default | 
|:--------------------------|:---------------------------------------------------------------------|--------:|
| `ELASTICSEARCH_USERNAME`  | Define the username for basic auth on elasticsearch integration      | `empty` |
| `ELASTICSEARCH_PASSWORD`  | Define the password for basic auth on elasticsearch integration      | `empty` |

## Examples

Here you have a complete example. More up-to-date one will always be maintained in 
`config/samples` directory [here](./config/samples)

```yaml
---
# Metrics service to check conditions for scaling up or down the cluster
metrics:

  # Prometheus integration
  prometheus:
    url: "http://127.0.0.1:8080"
    upCondition: "placeholder"
    downCondition: "placeholder"
    headers: {}

# Infrastructure service to interact with the cloud provider (GCP, AWS, etc.)
infrastructure:

  # GCP integration
  gcp:
    projectId: "placeholder"
    zone: "placeholder"
    migName: "placeholder"
    credentials_file: "placeholder"

# Target to control when scaling down the cluster
target:

  # Elasticsearch target service
  elasticsearch:
    url: "https://localhost:9200"
    user: "${ELASTICSEARCH_USER}"
    password: "${ELASTICSEARCH_PASSWORD}"
    sslInsecureSkipVerify: true

# Notifications service to send alerts to the team
notifications:

  # Slack integration
  slack:
    webhookUrl: "placeholder"

# General configuration for the autoscaler
autoscaler:
  debugMode: true
  defaultCooldownPeriodSec: 10
  scaledownCooldownPeriodSec: 10
  retiryIntervalSec: 10
  minSize: 1
  maxSize: 2

  # For advanced custom scaling configuration, when you want a different minSize and maxSize nodes for specific moments
  advancedCustomScalingConfiguration:
    - days: "2,3,4"
      hoursUTC: "5:00:00-8:00:00"
      minSize: 1
    - days: "6,7"
      minSize: 3
```

> ATTENTION:
> If you detect some mistake on the config, open an issue to fix it. This way we all will benefit

## How to deploy

This project provides binary files and Docker images to make it easy to be deployed wherever wanted

### Binaries

Binary files for most popular platforms will be added to the [releases](https://github.com/freepik-company/custom-vm-autoscaler/releases)

### Docker

Docker images can be found in GitHub's [packages](https://github.com/freepik-company/custom-vm-autoscaler/pkgs/container/custom-vm-autoscaler) 
related to this repository

> Do you need it in a different container registry? I think this is not needed, but if I'm wrong, please, let's discuss 
> it in the best place for that: an issue

## How to contribute

We are open to external collaborations for this project: improvements, bugfixes, whatever.

For doing it, open an issue to discuss the need of the changes, then:

- Fork the repository
- Make your changes to the code
- Open a PR and wait for review

The code will be reviewed and tested (always)

> We are developers and hate bad code. For that reason we ask you the highest quality
> on each line of code to improve this project on each iteration.