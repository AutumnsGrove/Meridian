# syntax=docker/dockerfile:1

# One binary with the web app embedded (decisions #8 and #57). Two stages do the work the box cannot:
# Node builds the SvelteKit app, Go cross-compiles the binary. The final image is distroless: the
# binary and nothing else, so there is no shell to reach for and no package manager to drift.
#
# Base images are pinned by digest and kept fresh by Dependabot. When bumping one, take the digest
# from the registry's index manifest, not from a tag.

FROM node:26-bookworm-slim@sha256:28147155d47dc31abe0a46e0d12b59b2fb3d0a2dcc5dc65fce76a074180b16fe AS web

# The version CI and the lockfile were written with. pnpm refuses a different major silently, so it is
# named here rather than left to corepack's default.
RUN npm install --global pnpm@10.32.1

WORKDIR /src/web
COPY web/package.json web/pnpm-lock.yaml ./
RUN pnpm install --frozen-lockfile
COPY web/ ./
RUN pnpm build

# $BUILDPLATFORM runs the compiler natively and cross-compiles for $TARGETARCH, so the arm64 image
# costs one compile instead of an emulated one.
FROM --platform=$BUILDPLATFORM golang:1.27.2-bookworm@sha256:5cf287a799e6b94384bad13d16b14904c531f51ba65792237e122ce42b392f61 AS build

WORKDIR /src

ARG TARGETOS
ARG TARGETARCH
ARG VERSION=dev
ARG COMMIT=dev
ARG BUILD_DATE=dev

COPY go.mod go.sum ./
RUN go mod download
COPY . ./

# The frontend is embedded through web/build, and the build stage owns that directory: a copy from the
# context would be a stale app masquerading as this build.
COPY --from=web /src/web/build ./web/build

RUN CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH go build \
		-trimpath \
		-buildvcs=false \
		-ldflags "-s -w \
			-X github.com/autumnsgrove/meridian/internal/cli.version=$VERSION \
			-X github.com/autumnsgrove/meridian/internal/cli.commit=$COMMIT \
			-X github.com/autumnsgrove/meridian/internal/cli.date=$BUILD_DATE" \
		-o /out/meridian ./cmd/meridian

FROM gcr.io/distroless/static-debian12:nonroot@sha256:afa5c872c891853ca7fcf1f12c3edb23f7eeef36189728842dd51042ff57f7ab

COPY --from=build /out/meridian /meridian

ENTRYPOINT ["/meridian", "run"]
