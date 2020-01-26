Kubernetes/Openshift manifests to deploy the gotiny with proxy.

Deployments:

    - gotiny: for the tinyURL service
    - proxy: proxy service serving as front for the app
    - redis: cache for storing the shortened URLs

Services:

    - proxy: internet facing endpoint for proxy service
    - gotiny: cluster facing endpoint for the gotiny app
    - redis: cluster facing for connecting redis and gitiny as inmemory cache