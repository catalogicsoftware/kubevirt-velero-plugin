properties([
    parameters([
        booleanParam(
            name: "BUILD_KUBEVIRT_PLUGIN",
            defaultValue: true,
            description: "Build and push kubevirt-velero-plugin image"
        ),
        booleanParam(
            name: "PUSH_TO_ACR",
            defaultValue: true,
            description: "Also push the image to Azure Container Registry (ACR)"
        ),
        booleanParam(
            name: "UPDATE_VELERO_REPO",
            defaultValue: true,
            description: "Update the kubevirt plugin image tag in plugins.ini of the velero repo (requires ACR push)"
        ),
        string(
            name: "VELERO_TARGET_BRANCH",
            defaultValue: "ks-v1.18.4.x",
            description: "Velero repo branch to update (defaults to the current branch name)"
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
    def baseVersion = "0.9.1"
    if (sourceBranch ==~ /^v\d+\.\d+\.\d+\.x$/) {
        baseVersion = sourceBranch.substring(1, sourceBranch.length() - 2)
    }

    def computedTag = "v${baseVersion}.${env.BUILD_NUMBER}"
    def imageTag = (params.IMAGE_TAG_OVERRIDE ?: "").trim() ? (params.IMAGE_TAG_OVERRIDE ?: "").trim() : computedTag

    def allowedBranches = ["v0.9.1.x"]
    def shouldBuild = (params.BUILD_KUBEVIRT_PLUGIN ?: false) && allowedBranches.contains(sourceBranch)

    // Internal registry
    def dockerRegistryInternal = env.DOCKER_REGISTRY_INTERNAL
    def dockerRegistryCredsInternal = env.DOCKER_REGISTRY_CREDENTIALS_INTERNAL
    def dockerPrefixInternal = "${dockerRegistryInternal}/catalogicsoftware"

    // ACR (set these in Jenkins env / folder properties)
    //   CLOUDCASA_ACR_REGISTRY         e.g. cloudcasaAgent.azurecr.io
    //   CLOUDCASA_ACR_CREDENTIALS_ID   Jenkins "Username with password" credential (SP id / secret)
    //   ACR_REPO_PREFIX                optional, defaults to catalogicsoftware
    def acrRegistry = env.CLOUDCASA_ACR_REGISTRY
    def acrCredsId = env.CLOUDCASA_ACR_CREDENTIALS_ID
    def acrPrefix = "${acrRegistry}/${env.ACR_REPO_PREFIX ?: 'catalogicsoftware'}"
    def shouldPushAcr = (params.PUSH_TO_ACR ?: false) && acrRegistry && acrCredsId

    def goBuilderImage = env.GO_BUILDER_IMAGE ?: "golang:1.26.0-bookworm"
    def imageName = "kubevirt-velero-plugin"
    def imageRef = "${dockerPrefixInternal}/${imageName}:${imageTag}"
    def acrImageRef = "${acrPrefix}/${imageName}:${imageTag}"

    def runBuildAndPush = {
        sh """
            set -eu

            docker run --rm \
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
                        OUTPUT_DIR=_output/bin/linux/amd64 GO111MODULE=on GOFLAGS=-mod=readonly \\
                        ./hack/build/build.sh
                    GOOS=linux GOARCH=arm64 PKG=kubevirt.io/kubevirt-velero-plugin BIN=kubevirt-velero-plugin \\
                        OUTPUT_DIR=_output/bin/linux/arm64 GO111MODULE=on GOFLAGS=-mod=readonly \\
                        ./hack/build/build.sh
                    cp Dockerfile _output/bin/linux/amd64/
                    cp Dockerfile _output/bin/linux/arm64/
                "

            docker buildx inspect multiarch >/dev/null 2>&1 || docker buildx create --name multiarch
            docker buildx use multiarch

            # Build and push each architecture with explicit platform metadata.
            docker buildx build \
                --platform linux/amd64 \
                --push \
                -f _output/bin/linux/amd64/Dockerfile \
                -t ${imageRef}-amd64 \
                _output/bin/linux/amd64
            docker buildx build \
                --platform linux/arm64 \
                --push \
                -f _output/bin/linux/arm64/Dockerfile \
                -t ${imageRef}-arm64 \
                _output/bin/linux/arm64

            # Merge the two single-arch images into the final multiarch manifest.
            docker buildx imagetools create --tag ${imageRef} ${imageRef}-amd64 ${imageRef}-arm64
        """
    }

    stage("Build and push kubevirt plugin image") {
        if (shouldBuild) {
            env.BUILDX_CONFIG = "${env.HOME}/.docker/buildx"

            docker.withRegistry("https://${dockerRegistryInternal}", dockerRegistryCredsInternal) {
                runBuildAndPush()

                if (shouldPushAcr) {
                    withCredentials([usernamePassword(
                        credentialsId: acrCredsId,
                        usernameVariable: "ACR_USER",
                        passwordVariable: "ACR_PASS"
                    )]) {
                        // Single quotes so the secret is expanded by the shell, not Groovy
                        sh 'echo "$ACR_PASS" | docker login ' + acrRegistry + ' -u "$ACR_USER" --password-stdin'
                        try {
                            // Copy the final multi-arch manifest from the internal registry to ACR
                            sh "docker buildx imagetools create --tag ${acrImageRef} ${imageRef}"
                        } finally {
                            sh "docker logout ${acrRegistry} || true"
                        }
                    }
                }
            }

            writeFile file: "image-version", text: "${imageTag}\n"
            archiveArtifacts artifacts: "image-version", onlyIfSuccessful: true
            currentBuild.description = "${imageName}:${imageTag}"
            echo "Pushed internal image: ${imageRef}"
            if (shouldPushAcr) {
                echo "Pushed ACR image: ${acrImageRef}"
            } else {
                echo "ACR push skipped (PUSH_TO_ACR=false or ACR registry / credentials not set)"
            }
        } else {
            echo "Skipping build for branch '${sourceBranch}'. Allowed: ${allowedBranches.join(', ')}"
        }
    }

    stage("Update plugins.ini in velero repo") {
        if (shouldBuild && shouldPushAcr && (params.UPDATE_VELERO_REPO ?: false)) {
            def veleroBranch = (params.VELERO_TARGET_BRANCH ?: "").trim() ?: sourceBranch
            def veleroRepoUrl = env.VELERO_REPO_URL   // e.g. https://github.com/catalogicsoftware/velero.git

            withCredentials([usernamePassword(
                credentialsId: 'github-access-token',
                usernameVariable: 'GIT_USER',
                passwordVariable: 'GIT_PASS'
            )]) {
                dir('velero-repo') {
                    git url: veleroRepoUrl, branch: veleroBranch, credentialsId: 'github-access-token'

                    sh """
                        git config user.name 'cloudcasabot'
                        git config user.email 'cloudcasabot@catalogicsoftware.com'
                    """

                    // Tag-only change: keeps the existing registry/org prefix in the file
                    // (cloudcasaAgent.azurecr.io/catalogicsoftware/...) and replaces just the tag.
                    withEnv(["NEW_TAG=${imageTag}", "IMAGE_NAME=${imageName}"]) {
                        sh '''
                            set -eu
                            sed -i -E "/^\\[plugin:kubevirt\\]/,/^\\[/ s|^(image[[:space:]]*=[[:space:]]*[^[:space:]]*${IMAGE_NAME}:)[^[:space:]]+|\\1${NEW_TAG}|" plugins.ini

                            grep -qF "${IMAGE_NAME}:${NEW_TAG}" plugins.ini || {
                                echo "Failed to update ${IMAGE_NAME} tag in plugins.ini" >&2
                                exit 1
                            }
                        '''
                    }

                    sh """
                        set -eu
                        if [ -n "\$(git status --porcelain)" ]; then
                            git add plugins.ini
                            git commit -m "ci: bump ${imageName} to ${imageTag} [${env.JOB_NAME} #${env.BUILD_NUMBER}]"
                            git pull --rebase https://\${GIT_USER}:\${GIT_PASS}@${veleroRepoUrl.replaceFirst('https://', '')} ${veleroBranch}
                            git push https://\${GIT_USER}:\${GIT_PASS}@${veleroRepoUrl.replaceFirst('https://', '')} ${veleroBranch}
                        else
                            echo "plugins.ini already up to date"
                        fi
                    """
                }
            }
            echo "Updated [plugin:kubevirt] in velero repo plugins.ini (${veleroBranch}) to ${imageName}:${imageTag}"
        } else {
            echo "Skipping velero repo update (needs a successful build, ACR push enabled, and UPDATE_VELERO_REPO=true)"
        }
    }
}
