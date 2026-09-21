# The landing page and its documentation, as a container.
#
# For a host that serves containers rather than reading Netlify's or
# Cloudflare's _redirects file — Dokploy, Nixploy, a plain Docker host. The
# rules those files describe are in the Caddyfile beside this one instead,
# because a static host that ignores them serves a site whose every
# documentation link 404s.
#
# Build from the REPOSITORY ROOT, not from deploy/site:
#
#   docker build -f deploy/site/Dockerfile -t runnerly-site .
#   docker run --rm -p 8080:80 runnerly-site
#
# The context has to be the root because the site is built from three
# things that live outside site/: the documentation in docs/, the logo in
# assets/, and install.sh, which the page tells people to curl.

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
