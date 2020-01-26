FROM centos:7

LABEL maintainer="Petr Kotas<petr@kotas.tech>"

RUN mkdir -p /usr/app
WORKDIR /usr/app

ADD ./proxy /usr/app/proxy

CMD ["./proxy"]
