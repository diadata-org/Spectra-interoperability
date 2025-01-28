Running hyperlane-relayer

 
1. Build the image locally
`docker build -t  dia-registry/hyperlane-relayer:latest . `
2. Push the docker image to IBM Cloud container repo
`docker push  dia-registry/hyperlane-relayer:latest`
3. Deploy the hyperlane-relayer to the cluster
`kubectl apply -f hyperlane-relayer.yaml`
4. Check if the deployment is runing as expected, inspect the logs, test a transaction