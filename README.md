# Stream Sniff

- [What is Stream Sniff](#what-is-stream-sniff)
- [Using](#using)
  - [OBS Broadcasting](#obs-broadcasting)
  - [FFmpeg Broadcasting](#ffmpeg-broadcasting)
- [Getting Started](#getting-started)
  - [Configuring](#configuring)
  - [Building From Source](#building-from-source)
  - [Docker](#docker)
  - [Docker Compose](#docker-compose)
- [Environment Variables](#environment-variables)
- [Design](#design)

## What is Stream Sniff

Stream Sniff analyzes and helps you debug your streaming video so you can get the best quality possible.
It inspects the incoming video and explains in plain language what you are sending, and how it could be better.

The goal is to help someone answer practical questions like:

- Is my video too compressed?
- Am I streaming at the right resolution?
- Which H.264 profile am I using?
- What settings would improve quality, compatibility, bitrate, or latency?

This was written as a sibling project of [Broadcast Box](https://github.com/glimesh/broadcast-box) and contains a lot of the same code.

## Using

### OBS

To use Stream Sniff with OBS, set your output to WebRTC and configure the WHIP target like this:

- Service: `WHIP`
- Server: `http://localhost:8080/api/whip`
- Bearer Token: any valid Bearer Token

### FFmpeg

The following command publishes a test feed to `http://localhost:8080/api/whip` with a Bearer Token of `ffmpeg-test`:

```shell
ffmpeg \
  -re \
  -f lavfi -i testsrc=size=1280x720 \
  -f lavfi -i sine=frequency=440 \
  -pix_fmt yuv420p -vcodec libx264 -profile:v baseline -r 25 -g 50 \
  -acodec libopus -ar 48000 -ac 2 \
  -f whip -authorization "ffmpeg-test" \
  "http://localhost:8080/api/whip"
```

> WHIP support is required for this example. FFmpeg added WHIP muxing in version 8.

## Getting Started

### Configuring

The backend loads [.env.production](./.env.production) by default. Set `APP_ENV=development` before starting the
process to load [.env.development](./.env.development) instead.

A reference [.env](./.env) is also included so you can copy settings between environments without modifying the default
runtime files.

### Building From Source

Go dependencies are installed automatically.

To run the server, execute:

```shell
go run .
```

If everything is wired correctly, startup logs will look similar to:

```console
2026/03/02 12:00:00 Loading `.env.production`
2026/03/02 12:00:00 Running HTTP Server at `:8080`
```

Open `http://<YOUR_HOST>:8080` for the minimal landing page. Publish to `http://<YOUR_HOST>:8080/api/whip`.

### Docker

A Docker image is included for local or server deployments.

Build it with:

```shell
docker build -t stream-sniff .
```

If you want to run locally with a fixed UDP mux port, use something like:

```shell
docker run \
  -e UDP_MUX_PORT=8080 \
  -e NAT_1_TO_1_IP=127.0.0.1 \
  -p 8080:8080 \
  -p 8080:8080/udp \
  stream-sniff
```

If you are running on a Linux host or cloud VM, `host` networking is usually simpler:

```shell
docker run --net=host -e INCLUDE_PUBLIC_IP_IN_NAT_1_TO_1_IP=yes stream-sniff
```

### Docker Compose

[docker-compose.yaml](./docker-compose.yaml) starts Stream Sniff in `host` network mode and sets a minimal production
environment.

To start it:

```shell
docker compose up -d
```

The WHIP endpoint will then be available at `http://<YOUR_HOST>:8080/api/whip`.

## Environment Variables

### Server Configuration

| Variable       | Description                                                                            |
| -------------- | -------------------------------------------------------------------------------------- |
| `APP_ENV`      | Set in the shell to `development` to load `.env.development` instead of `.env.production`. |
| `HTTP_ADDRESS` | Address for the HTTP server to bind to. The index page and WHIP endpoint both use it. |

### WebRTC & Networking

| Variable                             | Description                                                               |
| ------------------------------------ | ------------------------------------------------------------------------- |
| `INCLUDE_PUBLIC_IP_IN_NAT_1_TO_1_IP` | Automatically includes the server's public IP in NAT configuration.       |
| `NAT_1_TO_1_IP`                      | Manually specify IPs to announce, separated by `\|`.                      |
| `NAT_ICE_CANDIDATE_TYPE`             | Set to `srflx` to append NAT IPs as server reflexive candidates.          |
| `INTERFACE_FILTER`                   | Restrict UDP traffic to a specific network interface.                     |
| `NETWORK_TYPES`                      | List of WebRTC network types separated by `\|` such as `udp4\|udp6`.      |
| `INCLUDE_LOOPBACK_CANDIDATE`         | Enables ICE candidates on the loopback interface.                         |
| `UDP_MUX_PORT`                       | Port to multiplex all UDP traffic. Uses random ports by default.          |
| `UDP_MUX_PORT_WHIP`                  | Port to multiplex WHIP traffic only. Overrides `UDP_MUX_PORT` for WHIP.   |
| `TCP_MUX_ADDRESS`                    | Address to serve WebRTC traffic over TCP.                                 |
| `TCP_MUX_FORCE`                      | Forces WebRTC traffic to use TCP only.                                    |
| `APPEND_CANDIDATE`                   | Appends raw ICE candidate lines into the generated SDP answer.            |

### STUN Servers

| Variable       | Description                             |
| -------------- | --------------------------------------- |
| `STUN_SERVERS` | List of STUN servers separated by `\|`. |

These values are parsed by the Go backend and applied to WHIP `PeerConnection` configuration server-side.

### Debugging

| Variable             | Description                                 |
| -------------------- | ------------------------------------------- |
| `DEBUG_PRINT_OFFER`  | Prints WebRTC offers received from clients. |
| `DEBUG_PRINT_ANSWER` | Prints WebRTC answers sent to clients.      |

## Design

| Endpoint     | Description                                                                                           |
| ------------ | ----------------------------------------------------------------------------------------------------- |
| `/`          | Serves `index.html` from disk .                                                                       |
| `/api/whip`  | Accepts WHIP `POST` requests. Requires `Authorization: Bearer <bearerToken>` and returns an SDP answer. |
