# AVM FRITZ!Box Cloudflare DNS-service

This project has some simple goals:

- Offer a slim service without any additional service requirements
- Allow for two different combined strategies: Polling (through FRITZ!Box SOAP-API) and Pushing (FRITZ!Box Custom-DynDns setting).
- Allow multiple domains to be updated with new A (IPv4) and AAAA (IPv6) records
- Push those IP changes directly to CloudFlare DNS
- Deploy in docker compose

If this fits for you, skim over the CNAME workaround if this is a better solution for you, otherwise feel free to visit
the appropriate strategy section of this document and find out how to configure it correctly.

## CNAME record workaround

Before you try this service evaluate a cheap workaround, as it does not require dedicated hardware to run 24/7:

Have dynamic IP updates by using a CNAME record to your myfritz address, found in `Admin > Internet > MyFRITZ-Account`.
It should look like `[hash].myfritz.net`.

This basic example of a BIND DNS entry would make `intranet.example.com` auto update the current IP:

```
$TTL 60
$ORIGIN example.com.
intranet IN CNAME [hash].myfritz.net
```

Beware that this will expose your account hash to the outside world and depend on AVMs service availability.

## Strategies

### FRITZ!Box pushing

You can use this strategy if you have:

- access to the admin panel of the FRITZ!Box router.
- this services runs on a public interface towards the router.

In your `.env` file or your system environment variables you can be configured:

| Variable name | Description |
| --- | --- |
| DYNDNS_SERVER_BIND | required, network interface to bind to, i.e. `:8080` |
| DYNDNS_SERVER_USERNAME | optional, username for the DynDNS service |
| DYNDNS_SERVER_PASSWORD | optional, password for the DynDNS service |

Now configure the FRITZ!Box router to push IP changes towards this service. Log into the admin panel and go to
`Internet > Shares > DynDNS tab` and setup a  `Custom` provider:

| Property | Description / Value |
| --- | --- |
| Update-URL | http://[server-ip]/ip?v4=\<ipaddr\>&v6=\<ip6addr\>&prefix=\<ip6lanprefix\> |
| Domain | Enter at least one domain name so the router can probe if the update was successfully |
| Username | Enter '_' if  `DYNDNS_SERVER_USERNAME` env is unset |
| Password | Enter '_' if `DYNDNS_SERVER_PASSWORD` env is unset |

If you specified credentials you need to append them as additional GET parameters into the Update-URL like `&username=<user>&password=<pass>`.

### FRITZ!Box polling

You can use this strategy if you have:

- no access to the admin panel of the FRITZ!Box router.
- for whatever reasons the router can not push towards this service, but we can poll from it.
- you do not trust pushing

In your `.env` file or your system environment variables you can be configured:

| Variable name | Description |
| --- | --- |
| FRITZBOX_ENDPOINT_URL | optional, how can we reach the router, i.e. `http://fritz.box:49000`, the port should be 49000 anyway. |
| FRITZBOX_ENDPOINT_TIMEOUT | optional, a duration we give the router to respond, i.e. `10s`. |
| FRITZBOX_ENDPOINT_INTERVAL | optional, a duration how often we want to poll the WAN IPs from the router, i.e. `120s` |

You can try the endpoint URL in the browser to make sure you have the correct port, you should receive an `404 ERR_NOT_FOUND`.

## Cloudflare setup

To get your API Token do the following: Login to the cloudflare dashboard, go to `My Profile > API Tokens > Create Token > Edit zone DNS`, give to token some good name (e.g. "DDNS"), add all zones that the DDNS should be used for, click `Continue to summary` and `Create token`. Be sure to copy the token and add it to the config, you won't be able to see it again.

In your `.env` file or your system environment variables you can be configured:

| Variable name | Description |
| --- | --- |
| CLOUDFLARE_API_TOKEN | required, your Cloudflare API Token |
| CLOUDFLARE_ZONES_IPV4 | comma-separated list of domains to update with new IPv4 addresses |
| CLOUDFLARE_ZONES_IPV6 | comma-separated list of domains to update with new IPv6 addresses |
| CLOUDFLARE_API_EMAIL | deprecated, your Cloudflare account email |
| CLOUDFLARE_API_KEY | deprecated, your Cloudflare Global API key |

This service allows to update multiple records, an advanced example would be:

```env
CLOUDFLARE_ZONES_IPV4=ipv4.example.com,ip.example.com,server-01.dev.local
CLOUDFLARE_ZONES_IPV6=ipv6.example.com,ip.example.com,server-01.dev.local
```

Considering the example call `http://192.168.0.2:8080/ip?v4=127.0.0.1&v6=::1` every IPv4 listed zone would be updated to
`127.0.0.1` and every IPv6 listed one to `::1`.

## Register IPv6 for another device (port-forwarding)

IPv6 port-forwarding works differently and so if you want to use it you have to add the following configuration.

Warning: `FRITZBOX_ENDPOINT_URL` has to be set for this to work.

To access a device via IPv6 you need to add it's global IPv6 address to cloudflare, for this to be calculated you need to find out the local part of it's IP.
You can find out the local part of a device's IP, by going to the device's settings and looking at the `IPv6 Interface-ID`.
It should look something like this: `::1234:5678:90ab:cdef`

| Variable name | Description |
| --- | --- |
| DEVICE_LOCAL_ADDRESS_IPV6 | required, enter the local part of the device IP |

## Docker Compose Setup

_Instructions removed, there is no docker image for the project on the Docker Hub (yet?)_

## Docker Build

Change directory to project root (assumes you did git pull already) and execute image build process. Use the Dockerfile according to the CPU architecture you're working with. I will be using Raspberry Pi (Dockerfile.armhf) for this example, but Dockerfile.arm64 and Dockerfile.amd64 are also possible.

```
docker build -t silentexception/router-dyndns-helper --file Dockerfile.armhf .
docker image prune
```
__Raspberry Pi OS specific:__
Docker build will fail with a strange apk update error because the needed library libseccomp2 is too old. Browse to http://ftp.de.debian.org/debian/pool/main/libs/libseccomp/ and find the latest version. Today (17.06.2022), it was libseccomp2_2.5.4-1_armhf.deb.
```
wget http://ftp.de.debian.org/debian/pool/main/libs/libseccomp/libseccomp2_X.X.X-X_armhf.deb
sudo dpkg -i libseccomp2_X.X.X-X_armhf.deb
```
(in both commands replace X.X.X-X and use filename with latest version numbers you found)

After we have created the docker image, we need to create and start the container. Before that, create .env file with the settings you desire. You can use .env.dev as a template and add/remove desired features as needed.
```
cp .env.dev .env
nano .env
```

After you have settings dialed in, create and start the container. Substitute port 8888 with the desired free port the service will be available at the host machine.
```
docker create -t -p 8888:8080 --env-file .env --restart always --name router-dyndns-helper silentexception/router-dyndns-helper
docker start router-dyndns-helper
```

If you have previously created image and container and would like to update to latest version or update your settings, first stop and remove the container.
```
docker container stop router-dyndns-helper
docker container rm router-dyndns-helper
```
At this point if you just need to update the settings, update .env file as needed and execute docker create / docker start commands above. You're done.
If you would like to build new version then you need to remove docker image as well in order to build a new one. Don't forget about git pull also...
```
docker image rm docker image rm silentexception/router-dyndns-helper:latest
```
Now follow the steps above to do a docker build and docker create/ docker start.

Now we could configure the FRITZ!Box to `http://[docker-host-ip]:8888/ip?v4=<ipaddr>&v6=<ip6addr>&prefix=<ip6lanprefix>`.
If you leave `CLOUDFLARE_*` unconfigured, pushing to CloudFlare will be disabled. Useful for testing purposes, so you can navigate to `http://[docker-host-ip]:8888/ip?v4=127.0.0.1&v6=::1` to trigger update and review the logs.

```
docker logs router-dyndns-helper
```
