# The landing page and its documentation, as a container.
#
# This lives at the repository root on purpose. The site is built from four
# things — site/, docs/, assets/ and install.sh — so the build context has
# to be the root, and most hosts derive the context from wherever the
# Dockerfile sits. Put this in a subdirectory and the context becomes that
# subdirectory, every COPY below fails with "not found", and the error does
# not mention the context at all.
#
#   docker build -t runnerly-site .
#   docker run --rm -p 8080:80 runnerly-site
#
# The Caddy configuration beside it in deploy/site/ holds two rules that
# only matter in production: the fallback that makes /docs/agent resolve,
# and the content type that lets install.sh be piped into a shell.
#
# This is the site. The control plane's image is deploy/docker/Dockerfile.

FROM node:22-alpine AS build

WORKDIR /src

# Dependencies first, so a change to the page does not refetch them.
COPY site/package.json site/package-lock.json ./site/
RUN cd site && npm ci --no-fund --no-audit

# Everything the build reads. docs/ and assets/ are imported by the site,
# and install.sh is copied into dist/ by its postbuild step.
COPY site/ ./site/
COPY docs/ ./docs/
COPY assets/ ./assets/
COPY install.sh ./install.sh

RUN cd site && npm run build

# The build must have produced the installer the page advertises. Checking
# it here means a broken copy step fails the image rather than shipping a
# page whose one command 404s.
RUN test -f site/dist/install.sh \
	&& head -1 site/dist/install.sh | grep -q '^#!/bin/sh' \
	&& test -f site/dist/index.html

FROM caddy:2-alpine

COPY deploy/site/Caddyfile /etc/caddy/Caddyfile
COPY --from=build /src/site/dist /srv

EXPOSE 80

# Caddy's own health: the page itself. A container that starts but serves
# nothing should not read as healthy.
HEALTHCHECK --interval=30s --timeout=3s --start-period=5s \
	CMD wget -qO /dev/null http://127.0.0.1/ || exit 1
