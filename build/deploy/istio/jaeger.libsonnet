{
  all(metadata): {
    tracing_service: {
      apiVersion: 'v1',
      kind: 'Service',
      metadata: {
        name: 'tracing',
        namespace: 'istio-system',
        annotations: null,
        labels: {
          app: 'jaeger',
          release: 'istio',
        },
      },
      spec: {
        type: 'ClusterIP',
        ports: [
          {
            name: 'http-query',
            port: 80,
            protocol: 'TCP',
            targetPort: 16686,
          },
        ],
        selector: {
          app: 'jaeger',
        },
      },
    },
    zipkin_service: {
      apiVersion: 'v1',
      kind: 'Service',
      metadata: {
        name: 'zipkin',
        namespace: 'istio-system',
        labels: {
          app: 'jaeger',
          release: 'istio',
        },
      },
      spec: {
        ports: [
          {
            port: 9411,
            targetPort: 9411,
            protocol: 'TCP',
            name: 'http-query',
          },
        ],
        selector: {
          app: 'jaeger',
        },
      },
    },
    agent_service: {
      apiVersion: 'v1',
      kind: 'Service',
      metadata: {
        name: 'jaeger-agent',
        namespace: 'istio-system',
        labels: {
          app: 'jaeger',
          'jaeger-infra': 'agent-service',
          release: 'istio',
        },
      },
      spec: {
        ports: [
          {
            name: 'agent-zipkin-thrift',
            port: 5775,
            protocol: 'UDP',
            targetPort: 5775,
          },
          {
            name: 'agent-compact',
            port: 6831,
            protocol: 'UDP',
            targetPort: 6831,
          },
          {
            name: 'agent-binary',
            port: 6832,
            protocol: 'UDP',
            targetPort: 6832,
          },
        ],
        clusterIP: 'None',
        selector: {
          app: 'jaeger',
        },
      },
    },
    collector_service: {
      apiVersion: 'v1',
      kind: 'Service',
      metadata: {
        name: 'jaeger-collector',
        namespace: 'istio-system',
        labels: {
          app: 'jaeger',
          'jaeger-infra': 'collector-service',
          release: 'istio',
        },
      },
      spec: {
        ports: [
          {
            name: 'jaeger-collector-tchannel',
            port: 14267,
            protocol: 'TCP',
            targetPort: 14267,
          },
          {
            name: 'jaeger-collector-http',
            port: 14268,
            targetPort: 14268,
            protocol: 'TCP',
          },
          {
            name: 'jaeger-collector-grpc',
            port: 14250,
            targetPort: 14250,
            protocol: 'TCP',
          },
        ],
        selector: {
          app: 'jaeger',
        },
        type: 'ClusterIP',
      },
    },
    query_service: {
      apiVersion: 'v1',
      kind: 'Service',
      metadata: {
        name: 'jaeger-query',
        namespace: 'istio-system',
        annotations: null,
        labels: {
          app: 'jaeger',
          'jaeger-infra': 'jaeger-service',
          release: 'istio',
        },
      },
      spec: {
        ports: [
          {
            name: 'query-http',
            port: 16686,
            protocol: 'TCP',
            targetPort: 16686,
          },
        ],
        selector: {
          app: 'jaeger',
        },
      },
    },
    jaeger_deployment: {
      apiVersion: 'apps/v1',
      kind: 'Deployment',
      metadata: {
        name: 'istio-tracing',
        namespace: 'istio-system',
        labels: {
          app: 'jaeger',
          release: 'istio',
        },
      },
      spec: {
        selector: {
          matchLabels: {
            app: 'jaeger',
          },
        },
        template: {
          metadata: {
            labels: {
              app: 'jaeger',
              release: 'istio',
            },
            annotations: {
              'sidecar.istio.io/inject': 'false',
              'prometheus.io/scrape': 'true',
              'prometheus.io/port': '14269',
            },
          },
          spec: {
            containers: [
              {
                name: 'jaeger',
                image: 'docker.io/jaegertracing/all-in-one:1.14',
                imagePullPolicy: 'IfNotPresent',
                ports: [
                  {
                    containerPort: 9411,
                  },
                  {
                    containerPort: 16686,
                  },
                  {
                    containerPort: 14250,
                  },
                  {
                    containerPort: 14267,
                  },
                  {
                    containerPort: 14268,
                  },
                  {
                    containerPort: 14269,
                  },
                  {
                    containerPort: 5775,
                    protocol: 'UDP',
                  },
                  {
                    containerPort: 6831,
                    protocol: 'UDP',
                  },
                  {
                    containerPort: 6832,
                    protocol: 'UDP',
                  },
                ],
                env: [
                  {
                    name: 'POD_NAMESPACE',
                    valueFrom: {
                      fieldRef: {
                        apiVersion: 'v1',
                        fieldPath: 'metadata.namespace',
                      },
                    },
                  },
                  {
                    name: 'BADGER_EPHEMERAL',
                    value: 'false',
                  },
                  {
                    name: 'SPAN_STORAGE_TYPE',
                    value: 'badger',
                  },
                  {
                    name: 'BADGER_DIRECTORY_VALUE',
                    value: '/badger/data',
                  },
                  {
                    name: 'BADGER_DIRECTORY_KEY',
                    value: '/badger/key',
                  },
                  {
                    name: 'COLLECTOR_ZIPKIN_HTTP_PORT',
                    value: '9411',
                  },
                  {
                    name: 'MEMORY_MAX_TRACES',
                    value: '50000',
                  },
                  {
                    name: 'QUERY_BASE_PATH',
                    value: '/jaeger',
                  },
                ],
                livenessProbe: {
                  httpGet: {
                    path: '/',
                    port: 14269,
                  },
                },
                readinessProbe: {
                  httpGet: {
                    path: '/',
                    port: 14269,
                  },
                },
                volumeMounts: [
                  {
                    name: 'data',
                    mountPath: '/badger',
                  },
                ],
                resources: {
                  requests: {
                    cpu: '10m',
                  },
                },
              },
            ],
            affinity: {
              nodeAffinity: {
                requiredDuringSchedulingIgnoredDuringExecution: {
                  nodeSelectorTerms: [
                    {
                      matchExpressions: [
                        {
                          key: 'beta.kubernetes.io/arch',
                          operator: 'In',
                          values: [
                            'amd64',
                            'ppc64le',
                            's390x',
                          ],
                        },
                      ],
                    },
                  ],
                },
                preferredDuringSchedulingIgnoredDuringExecution: [
                  {
                    weight: 2,
                    preference: {
                      matchExpressions: [
                        {
                          key: 'beta.kubernetes.io/arch',
                          operator: 'In',
                          values: [
                            'amd64',
                          ],
                        },
                      ],
                    },
                  },
                  {
                    weight: 2,
                    preference: {
                      matchExpressions: [
                        {
                          key: 'beta.kubernetes.io/arch',
                          operator: 'In',
                          values: [
                            'ppc64le',
                          ],
                        },
                      ],
                    },
                  },
                  {
                    weight: 2,
                    preference: {
                      matchExpressions: [
                        {
                          key: 'beta.kubernetes.io/arch',
                          operator: 'In',
                          values: [
                            's390x',
                          ],
                        },
                      ],
                    },
                  },
                ],
              },
            },
            volumes: [
              {
                name: 'data',
                emptyDir: {},
              },
            ],
          },
        },
      },
    },
  },
}