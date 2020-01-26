FROM centos:7

LABEL maintainer="Petr Kotas<petr@kotas.tech>"

RUN mkdir -p /usr/app
WORKDIR /usr/app

ADD ./tiny /usr/app/tiny
ADD ./tiny.html /usr/app/tiny.html
ADD ./result.html /usr/app/result.html

CMD ["./tiny"]
