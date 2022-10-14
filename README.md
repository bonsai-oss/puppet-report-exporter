# puppet-report-exporter

Export Puppet report logs to prometheus for gathering information about your runs.
The intention is to see any error inside your puppet runs even if the run status itself is successful.

## Usage

Currently, the following modes are supported:
* `puppetdb`: Use PuppetDB to get the reports  
Configure *plain http* puppetdb api access uri within `PUPPETDB_URI` environment variable or via cli parameter.

* `http-report`: Use Puppets http report processor to get the reports  
Configure the [reports](https://puppet.com/docs/puppet/7/configuration.html#reports) and [reporturl](https://puppet.com/docs/puppet/7/configuration.html#reporturl) settings in your puppet.conf.


```
puppet-report-exporter

Flags:
  --help                                          Show context-sensitive help (also try --help-long and --help-man).
  --web.listen-address=":9115"                    Address to listen on for web interface and telemetry
  --report.listen-address=":9116"                 Address to listen on for report submission
  --mode=puppetdb                                 Mode of operation.
  --puppetdb.api-address="http://puppetdb:8081"   Address of the PuppetDB API
  --puppetdb.initial-fetch                        Fetch all nodes on startup
```

Running inside Docker:

```shell
$ docker run --rm -p 127.0.0.1:9115:9115 -it registry.gitlab.com/bonsai-oss/exporter/puppet-report-exporter:latest --help
```
