properties([
    parameters([
        booleanParam(
            name: "BUILD_KUBEVIRT_PLUGIN",
            defaultValue: true,
            description: "Build and push kubevirt-velero-plugin image"
        ),
        string(
            name: "IMAGE_TAG_OVERRIDE",
            defaultValue: "",
            description: "Optional explicit image tag override"
        )
    ])
])

node("cloudcasa-build") {
    stage("Checkout") {
        cleanWs()
        checkout scm
    }

    def sourceBranch = env.BRANCH_NAME ?: "unknown"
    def sanitizedBranch = sourceBranch.replaceAll('[^0-9A-Za-z-]', '-')

    // Follow amds-veleroplugin style: <baseVersion>-<branch>.<buildNumber>
    def baseVersion = "0.7.1"
    if (sourceBranch ==~ /^v\d+\.\d+\.\d+\.x$/) {
        baseVersion = sourceBranch.substring(1, sourceBranch.length() - 2)
    }

    def computedTag = "${baseVersion}-${sanitizedBranch}.${env.BUILD_NUMBER}"
    def imageTag = (params.IMAGE_TAG_OVERRIDE ?: "").trim() ? (params.IMAGE_TAG_OVERRIDE ?: "").trim() : computedTag

    def allowedBranches = ["v0.7.1.x", "jg-KUBEDR-7845"]
    def shouldBuild = (params.BUILD_KUBEVIRT_PLUGIN ?: false) && allowedBranches.contains(sourceBranch)

    def dockerRegistryInternal = env.DOCKER_REGISTRY_INTERNAL
    def dockerRegistryCredsInternal = env.DOCKER_REGISTRY_CREDENTIALS_INTERNAL
    def dockerPrefixInternal = "${dockerRegistryInternal}/catalogicsoftware"
    def goBuilderImage = env.GO_BUILDER_IMAGE ?: "golang:1.26.0-bookworm"
    def imageName = "kubevirt-velero-plugin"
    def imageRef = "${dockerPrefixInternal}/${imageName}:${imageTag}"

    stage("Build and push kubevirt plugin image") {
        if (shouldBuild) {
            env.BUILDX_CONFIG = "${env.HOME}/.docker/buildx"
            docker.withRegistry("https://${dockerRegistryInternal}", dockerRegistryCredsInternal) {
                sh """
                    set -eu

                    docker run --rm \
                        -u \$(id -u):\$(id -g) \
                        -v \${WORKSPACE}:/workspace \
                        -w /workspace \
                        -e GOPATH=/workspace/.go \
                        -e GOMODCACHE=/workspace/.go/pkg/mod \
                        -e GOCACHE=/workspace/.go/cache \
                        ${goBuilderImage} bash -c "
                            set -eu
                            go version
                            mkdir -p _output/bin/linux/amd64 _output/bin/linux/arm64
                            GOOS=linux GOARCH=amd64 PKG=kubevirt.io/kubevirt-velero-plugin BIN=kubevirt-velero-plugin \\
                                OUTPUT_DIR=\$(pwd)/_output/bin/linux/amd64 GO111MODULE=on GOFLAGS=-mod=readonly \\
                                ./hack/build/build.sh
                            GOOS=linux GOARCH=arm64 PKG=kubevirt.io/kubevirt-velero-plugin BIN=kubevirt-velero-plugin \\
                                OUTPUT_DIR=\$(pwd)/_output/bin/linux/arm64 GO111MODULE=on GOFLAGS=-mod=readonly \\
                                ./hack/build/build.sh
                        "

                    cp Dockerfile _output/bin/linux/amd64/Dockerfile
                    cp Dockerfile _output/bin/linux/arm64/Dockerfile

                    docker buildx inspect multiarch >/dev/null 2>&1 || docker buildx create --name multiarch
                    docker buildx use multiarch

                    docker buildx build \
                        --platform linux/amd64 \
                        -t ${imageRef}-amd64 \
                        -f _output/bin/linux/amd64/Dockerfile \
                        --push \
                        _output/bin/linux/amd64

                    docker buildx build \
                        --platform linux/arm64 \
                        -t ${imageRef}-arm64 \
                        -f _output/bin/linux/arm64/Dockerfile \
                        --push \
                        _output/bin/linux/arm64

                    docker buildx imagetools create \
                        --tag ${imageRef} \
                        ${imageRef}-amd64 \
                        ${imageRef}-arm64
                """
            }

            writeFile file: "image-version", text: "${imageTag}\n"
            archiveArtifacts artifacts: "image-version", onlyIfSuccessful: true
            currentBuild.description = "${imageName}:${imageTag}"
            echo "Pushed internal image: ${imageRef}"
        } else {
            echo "Skipping build for branch '${sourceBranch}'. Allowed: ${allowedBranches.join(', ')}"
        }
    }
}
