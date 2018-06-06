/*
Copyright 2017 The Kubernetes Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package config

const ClusterAPIDeployConfigTemplate = `
apiVersion: apiregistration.k8s.io/v1beta1
kind: APIService
metadata:
  name: v1alpha1.cluster.k8s.io
  labels:
    api: clusterapi
    apiserver: "true"
spec:
  version: v1alpha1
  group: cluster.k8s.io
  groupPriorityMinimum: 2000
  priority: 200
  service:
    name: clusterapi
    namespace: default
  versionPriority: 10
  caBundle: {{ .CABundle }}
---
apiVersion: v1
kind: Service
metadata:
  name: clusterapi
  namespace: default
  labels:
    api: clusterapi
    apiserver: "true"
spec:
  ports:
  - port: 443
    protocol: TCP
    targetPort: 443
  selector:
    api: clusterapi
    apiserver: "true"
---
apiVersion: apps/v1beta1
kind: Deployment
metadata:
  name: clusterapi
  namespace: default
  labels:
    api: clusterapi
    apiserver: "true"
spec:
  replicas: 1
  template:
    metadata:
      labels:
        api: clusterapi
        apiserver: "true"
    spec:
      nodeSelector:
        node-role.kubernetes.io/master: ""
      tolerations:
      - effect: NoSchedule
        key: node-role.kubernetes.io/master
      - key: CriticalAddonsOnly
        operator: Exists
      - effect: NoExecute
        key: node.alpha.kubernetes.io/notReady
        operator: Exists
      - effect: NoExecute
        key: node.alpha.kubernetes.io/unreachable
        operator: Exists
      containers:
      - name: apiserver
        image: {{ .APIServerImage }}
        volumeMounts:
        - name: cluster-apiserver-certs
          mountPath: /apiserver.local.config/certificates
          readOnly: true
        - name: config
          mountPath: /etc/kubernetes
        - name: certs
          mountPath: /etc/ssl/certs
        command:
        - "./apiserver"
        args:
        - "--etcd-servers=http://etcd-clusterapi-svc:2379"
        - "--tls-cert-file=/apiserver.local.config/certificates/tls.crt"
        - "--tls-private-key-file=/apiserver.local.config/certificates/tls.key"
        - "--audit-log-path=-"
        - "--audit-log-maxage=0"
        - "--audit-log-maxbackup=0"
        - "--authorization-kubeconfig=/etc/kubernetes/admin.conf"
        - "--kubeconfig=/etc/kubernetes/admin.conf"
        resources:
          requests:
            cpu: 100m
            memory: 20Mi
          limits:
            cpu: 100m
            memory: 30Mi
      - name: controller-manager
        image: {{ .ControllerManagerImage }}
        volumeMounts:
          - name: config
            mountPath: /etc/kubernetes
          - name: certs
            mountPath: /etc/ssl/certs
        command:
        - "./controller-manager"
        args:
        - --kubeconfig=/etc/kubernetes/admin.conf
        resources:
          requests:
            cpu: 100m
            memory: 20Mi
          limits:
            cpu: 100m
            memory: 30Mi
      - name: vsphere-machine-controller
        image: {{ .MachineControllerImage }}
        volumeMounts:
          - name: config
            mountPath: /etc/kubernetes
          - name: certs
            mountPath: /etc/ssl/certs
          - name: machines-stage
            mountPath: /tmp/cluster-api/machines/
          - name: sshkeys
            mountPath: /root/.ssh
          - name: named-machines
            mountPath: /etc/named-machines
        command:
        - "./vsphere-machine-controller"
        args:
        - --kubeconfig=/etc/kubernetes/admin.conf
        - --token={{ .Token }}
        - --namedmachines=/etc/named-machines/vsphere_named_machines.yaml
        resources:
          requests:
            cpu: 200m
            memory: 200Mi
          limits:
            cpu: 400m
            memory: 500Mi
      volumes:
      - name: cluster-apiserver-certs
        secret:
          secretName: cluster-apiserver-certs
      - name: config
        hostPath:
          path: /etc/kubernetes
      - name: certs
        hostPath:
          path: /etc/ssl/certs
      - name: machines-stage
        hostPath:
          path: /tmp/cluster-api/machines/
      - name: sshkeys
        hostPath:
          path: /home/ubuntu/.ssh
      - name: named-machines
        configMap:
          name: named-machines
---
apiVersion: apps/v1beta1
kind: StatefulSet
metadata:
  name: etcd-clusterapi
  namespace: default
spec:
  serviceName: "etcd"
  replicas: 1
  template:
    metadata:
      labels:
        app: etcd
    spec:
      nodeSelector:
        node-role.kubernetes.io/master: ""
      tolerations:
      - effect: NoSchedule
        key: node-role.kubernetes.io/master
      - key: CriticalAddonsOnly
        operator: Exists
      - effect: NoExecute
        key: node.alpha.kubernetes.io/notReady
        operator: Exists
      - effect: NoExecute
        key: node.alpha.kubernetes.io/unreachable
        operator: Exists
      volumes:
      - hostPath:
          path: /var/lib/etcd2
          type: DirectoryOrCreate
        name: etcd-data-dir
      terminationGracePeriodSeconds: 10
      containers:
      - name: etcd
        image: quay.io/coreos/etcd:latest
        imagePullPolicy: Always
        resources:
          requests:
            cpu: 100m
            memory: 20Mi
          limits:
            cpu: 100m
            memory: 30Mi
        env:
        - name: ETCD_DATA_DIR
          value: /etcd-data-dir
        command:
        - /usr/local/bin/etcd
        - --listen-client-urls
        - http://0.0.0.0:2379
        - --advertise-client-urls
        - http://localhost:2379
        ports:
        - containerPort: 2379
        volumeMounts:
        - name: etcd-data-dir
          mountPath: /etcd-data-dir
        readinessProbe:
          httpGet:
            port: 2379
            path: /health
          failureThreshold: 1
          initialDelaySeconds: 10
          periodSeconds: 10
          successThreshold: 1
          timeoutSeconds: 2
        livenessProbe:
          httpGet:
            port: 2379
            path: /health
          failureThreshold: 3
          initialDelaySeconds: 10
          periodSeconds: 10
          successThreshold: 1
          timeoutSeconds: 2
---
apiVersion: v1
kind: Service
metadata:
  name: etcd-clusterapi-svc
  namespace: default
  labels:
    app: etcd
spec:
  ports:
  - port: 2379
    name: etcd
    targetPort: 2379
  selector:
    app: etcd
---
apiVersion: v1
kind: Secret
type: kubernetes.io/tls
metadata:
  name: cluster-apiserver-certs
  namespace: default
  labels:
    api: clusterapi
    apiserver: "true"
data:
  tls.crt: {{ .TLSCrt }}
  tls.key: {{ .TLSKey }}
`
