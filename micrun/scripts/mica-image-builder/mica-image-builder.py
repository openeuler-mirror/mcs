#!/usr/bin/env python3

import argparse
import sys
from pathlib import Path

sys.path.append(str(Path(__file__).parent))
from mica_label_manager import MicaLabelManager

# Default file paths in container
DEFAULT_FIRMWARE_PATH = "/firmware.elf"
DEFAULT_XEN_BIN_IMG_PATH = "/image.bin"


class MicaImageBuilder:
    def __init__(self, init_docker: bool = True):
        self.registry = "localhost:5000"
        self.pedestal = None
        self.os_type = None
        self.firmware_path = None
        self.xen_image_path = None
        self.image_name = None
        self.image_description = None
        self.zephyr_version = "3.7.1"
        self.uniproton_version = "latest"
        self.label_manager = MicaLabelManager()
        self.dry_run = not init_docker
        self.platform = None

        if init_docker:
            try:
                import docker

                self.client = docker.from_env()
            except ImportError:
                print(
                    "Error: docker-py not installed. Install with: pip install docker"
                )
                sys.exit(1)
            except Exception as e:
                print(f"Error initializing Docker client: {e}")
                sys.exit(1)
        else:
            self.client = None

    def resolve_platforms(self, platform_str):
        """Resolve platform string to actual platforms"""
        if not platform_str:
            return None  # Default to native build (no platform flag)
        if platform_str == "all":
            return "linux/amd64,linux/arm64"
        return platform_str

    def setup_registry(self):
        print("Setting up local registry...")

        try:
            existing_registry = self.client.containers.get("mica-registry")
            if existing_registry.status == "running":
                print("Registry container already running")
                return True
            else:
                existing_registry.remove()
        except:
            pass

        import socket

        available_ports = [5000, 5001, 5002, 5003]
        selected_port = None

        for port in available_ports:
            with socket.socket(socket.AF_INET, socket.SOCK_STREAM) as s:
                if s.connect_ex(("localhost", port)) != 0:
                    selected_port = port
                    break

        if selected_port is None:
            print("No available ports found for registry")
            return False

        self.registry = f"localhost:{selected_port}"

        try:
            try:
                self.client.images.get("registry:2")
            except:
                print("Pulling registry:2 image...")
                self.client.images.pull("registry:2")

            self.client.containers.run(
                "registry:2",
                name="mica-registry",
                ports={"5000/tcp": selected_port},
                detach=True,
                remove=True,
            )
            print(f"Registry started on {self.registry}")
            return True
        except Exception as e:
            print(f"Failed to start registry: {e}")
            return False

    def select_pedestal(self):
        print("\nSelect pedestal type:")
        print("1. xen")
        print("2. openamp")

        while True:
            choice = input("Enter choice (1-2): ").strip()
            if choice == "1":
                self.pedestal = "xen"
                break
            elif choice == "2":
                self.pedestal = "openamp"
                break
            else:
                print("Invalid choice")

    def select_os_type(self):
        print("\nSelect OS type:")
        print("1. zephyr")
        print("2. uniproton")

        while True:
            choice = input("Enter choice (1-2): ").strip()
            if choice == "1":
                self.os_type = "zephyr"
                break
            elif choice == "2":
                self.os_type = "uniproton"
                break
            else:
                print("Invalid choice")

    def select_platform(self):
        """Select build platform(s) for multi-architecture support"""
        print("\nSelect build platform:")
        print("1. Native build (default)")
        print("2. amd64 only (linux/amd64)")
        print("3. arm64 only (linux/arm64)")
        print("4. amd64 + arm64 (linux/amd64,linux/arm64)")
        print("5. All supported platforms (linux/amd64,linux/arm64)")

        while True:
            choice = input("Enter choice (1-5, default: 1): ").strip()
            if not choice or choice == "1":
                self.platform = None  # Native build
                break
            elif choice == "2":
                self.platform = "linux/amd64"
                break
            elif choice == "3":
                self.platform = "linux/arm64"
                break
            elif choice == "4":
                self.platform = "linux/amd64,linux/arm64"
                break
            elif choice == "5":
                self.platform = self.resolve_platforms("all")
                break
            else:
                print("Invalid choice")

    def select_os_versions(self):
        # Get default versions from TOML config
        default_versions = {"zephyr": "3.7.1", "uniproton": "latest"}

        # Try to get versions from default-compatibility section
        try:
            if "default-compatibility" in self.label_manager.labels_config:
                if (
                    "zephyr"
                    in self.label_manager.labels_config["default-compatibility"]
                ):
                    default_versions["zephyr"] = self.label_manager.labels_config[
                        "default-compatibility"
                    ]["zephyr"]
                if (
                    "uniproton"
                    in self.label_manager.labels_config["default-compatibility"]
                ):
                    default_versions["uniproton"] = self.label_manager.labels_config[
                        "default-compatibility"
                    ]["uniproton"]
        except:
            pass

        print(f"\nEnter OS versions (press Enter for defaults):")

        zephyr_version = input(
            f"Zephyr version (default: {default_versions['zephyr']}): "
        ).strip()
        self.zephyr_version = (
            zephyr_version if zephyr_version else default_versions["zephyr"]
        )

        uniproton_version = input(
            f"Uniproton version (default: {default_versions['uniproton']}): "
        ).strip()
        self.uniproton_version = (
            uniproton_version if uniproton_version else default_versions["uniproton"]
        )

    def select_image_files(self):
        print("\nSelect firmware file:")
        # Search in scripts directory (parent of mica-image-builder)
        scripts_dir = Path(__file__).absolute().parent.parent
        firmware_files = list(scripts_dir.glob("**/*.elf")) + list(
            scripts_dir.glob("**/*.bin")
        )
        # Remove duplicates and sort
        firmware_files = sorted(set(firmware_files), key=lambda x: x.name)

        if not firmware_files:
            print("No firmware files found. Please provide path manually.")
            cwd = Path.cwd()
            while True:
                self.firmware_path = input(
                    f"firmware file path: (current {cwd})"
                ).strip()
                firmware_path_obj = Path(self.firmware_path)
                if firmware_path_obj.exists():
                    break
                else:
                    print(
                        f"File not found: {self.firmware_path}. Please enter a valid path."
                    )
        else:
            for i, file in enumerate(firmware_files, 1):
                print(f"{i}. {file}")
            print(f"{len(firmware_files) + 1}. Enter custom path")

            while True:
                try:
                    choice = int(
                        input(f"Select firmware file (1-{len(firmware_files) + 1}): ")
                    )
                    if 1 <= choice <= len(firmware_files):
                        selected_file = firmware_files[choice - 1]
                        # Convert absolute path to relative path from scripts directory
                        scripts_dir = Path(__file__).absolute().parent.parent
                        self.firmware_path = str(selected_file.relative_to(scripts_dir))
                        break
                    elif choice == len(firmware_files) + 1:
                        cwd = Path.cwd()
                        while True:
                            self.firmware_path = input(
                                f"Enter firmware file path: (current {cwd})"
                            ).strip()
                            firmware_path_obj = Path(self.firmware_path)
                            if firmware_path_obj.exists():
                                # Convert to relative path if it's an absolute path
                                if firmware_path_obj.is_absolute():
                                    scripts_dir = (
                                        Path(__file__).absolute().parent.parent
                                    )
                                    try:
                                        self.firmware_path = str(
                                            firmware_path_obj.relative_to(scripts_dir)
                                        )
                                    except ValueError:
                                        # If file is not under scripts directory, keep absolute path
                                        pass
                                break
                            else:
                                print(
                                    f"File not found: {self.firmware_path}. Please enter a valid path."
                                )
                        break
                    else:
                        print("Invalid choice")
                except ValueError:
                    print("Please enter a number")

        if self.pedestal == "xen":
            print("\nSelect Xen image file:")
            # Search in scripts directory (parent of mica-image-builder)
            scripts_dir = Path(__file__).absolute().parent.parent
            xen_files = list(scripts_dir.glob("**/*.bin"))
            # Remove duplicates and sort
            xen_files = sorted(set(xen_files), key=lambda x: x.name)

            if not xen_files:
                print("No Xen image files found. Please provide path manually.")
                while True:
                    self.xen_image_path = input("Enter Xen image file path: ").strip()
                    xen_path_obj = Path(self.xen_image_path)
                    if xen_path_obj.exists():
                        break
                    else:
                        print(
                            f"File not found: {self.xen_image_path}. Please enter a valid path."
                        )
            else:
                for i, file in enumerate(xen_files, 1):
                    print(f"{i}. {file}")
                print(f"{len(xen_files) + 1}. Enter custom path")

                while True:
                    try:
                        choice = int(
                            input(f"Select Xen image file (1-{len(xen_files) + 1}): ")
                        )
                        if 1 <= choice <= len(xen_files):
                            selected_file = xen_files[choice - 1]
                            # Convert absolute path to relative path from scripts directory
                            scripts_dir = Path(__file__).absolute().parent.parent
                            self.xen_image_path = str(
                                selected_file.relative_to(scripts_dir)
                            )
                            break
                        elif choice == len(xen_files) + 1:
                            while True:
                                self.xen_image_path = input(
                                    "Enter Xen image file path: "
                                ).strip()
                                xen_path_obj = Path(self.xen_image_path)
                                if xen_path_obj.exists():
                                    # Convert to relative path if it's an absolute path
                                    if xen_path_obj.is_absolute():
                                        scripts_dir = (
                                            Path(__file__).absolute().parent.parent
                                        )
                                        try:
                                            self.xen_image_path = str(
                                                xen_path_obj.relative_to(scripts_dir)
                                            )
                                        except ValueError:
                                            # If file is not under scripts directory, keep absolute path
                                            pass
                                    break
                                else:
                                    print(
                                        f"File not found: {self.xen_image_path}. Please enter a valid path."
                                    )
                            break
                        else:
                            print("Invalid choice")
                    except ValueError:
                        print("Please enter a number")

    def get_image_description(self):
        # Get custom image description with default
        default_description = f"Mica {self.os_type} Container Image"
        description = input(
            f"Enter image description (default: {default_description}): "
        ).strip()
        if not description:
            description = default_description
        return description

    def get_image_names(self):
        # Three-part image naming: registry, app name, version
        print(f"\nImage naming (current registry: {self.registry})")

        # Registry input
        registry_input = input(f"Enter registry (default: {self.registry}): ").strip()
        if registry_input:
            self.registry = registry_input

        # App name input
        default_app = "app"
        app_name = input(f"Enter app name (default: {default_app}): ").strip()
        if not app_name:
            app_name = default_app

        # Version input
        default_version = "0.1"
        version = input(f"Enter version (default: {default_version}): ").strip()
        if not version:
            version = default_version

        # Construct final image name: {registry}/mica-{os}-{app}:{pedestal}-{version}
        self.image_name = (
            f"{self.registry}/mica-{self.os_type}-{app_name}:{self.pedestal}-{version}"
        )
        print(f"Final image name: {self.image_name}")

        return self.image_name

    def generate_dockerfile_final(self):
        scratch_labels, scratch_annotations = (
            self.label_manager.get_scratch_labels_and_annotations(
                self.pedestal, self.os_type
            )
        )
        final_labels, final_annotations = (
            self.label_manager.get_final_labels_and_annotations(
                self.pedestal,
                self.os_type,
                xen_image_path=self.xen_image_path if self.pedestal == "xen" else None,
                firmware_path="/firmware.elf",  # Container path, not source path
                custom_description=self.image_description,
                zephyr_version=getattr(self, "zephyr_version", "3.7.1"),
                uniproton_version=getattr(self, "uniproton_version", "latest"),
            )
        )

        labels = {**scratch_labels, **final_labels}
        annotations = {**scratch_annotations, **final_annotations}
        labels_formatted = self.label_manager.format_docker_labels(labels)

        dockerfile_content = f"""FROM scratch

ARG FIRMWARE_BUNDLE_PATH="/firmware.elf"
"""

        if self.pedestal == "xen":
            dockerfile_content += f'ARG XEN_BIN_IMG="/image.bin"\n'

        dockerfile_content += f"""
{labels_formatted}

ADD {self.firmware_path} ${{FIRMWARE_BUNDLE_PATH}}
"""

        if self.pedestal == "xen":
            dockerfile_content += f"ADD {self.xen_image_path} ${{XEN_BIN_IMG}}\n"

        return dockerfile_content.encode("utf-8"), labels, annotations

    def build_image_with_dockerfile(
        self, dockerfile_content, tag, build_context=None, annotations=None
    ):
        print(f"Building image: {tag}")

        # Default build context is the repository scripts directory.
        if build_context is None:
            build_context = str(Path(__file__).absolute().parent.parent)

        print(f"Build context: {build_context}")
        print("Dockerfile content preview:")
        print(dockerfile_content.decode("utf-8"))
        print("--- End of Dockerfile content ---")

        # In dry-run, just print and continue to show CLI commands that would be executed.
        if self.dry_run:
            print("[dry-run] Dockerfile printed above.")
            return self._build_with_docker_cli(
                dockerfile_content, tag, build_context, annotations=annotations
            )

        # Use Docker SDK with a Dockerfile path inside the build context.
        # Do not pass raw bytes as fileobj because the SDK expects a tar stream.
        import os
        import tempfile

        dockerfile_name = None
        try:
            if annotations:
                return self._build_with_docker_cli(
                    dockerfile_content,
                    tag,
                    build_context,
                    annotations=annotations,
                )
            # Create a uniquely named Dockerfile in build context
            fd, tmp_path = tempfile.mkstemp(
                prefix=".mica.Dockerfile.", dir=build_context
            )
            os.close(fd)
            with open(tmp_path, "wb") as f:
                f.write(dockerfile_content)

            dockerfile_name = os.path.basename(tmp_path)

            image, logs = self.client.images.build(
                path=build_context,
                dockerfile=dockerfile_name,
                tag=tag,
                rm=True,
                forcerm=True,
            )

            for chunk in logs:
                if isinstance(chunk, dict) and "stream" in chunk:
                    line = chunk["stream"].strip()
                    if line:
                        print(f"  {line}")

            print(f"Successfully built {tag}")
            return True
        except Exception as e:
            print(f"Docker SDK build failed for {tag}: {e}")
            print("Falling back to Docker CLI...")
            return self._build_with_docker_cli(
                dockerfile_content, tag, build_context, annotations=annotations
            )
        finally:
            # Best-effort cleanup of the temporary Dockerfile
            if dockerfile_name:
                try:
                    os.remove(os.path.join(build_context, dockerfile_name))
                except OSError:
                    pass

    def _build_with_docker_cli(
        self, dockerfile_content, tag, build_context, annotations=None
    ):
        """Fallback method to build using Docker CLI directly"""
        import os
        import subprocess
        import tempfile

        print(f"Building with Docker CLI: {tag}")

        # Create temporary Dockerfile
        with tempfile.NamedTemporaryFile(
            mode="w", suffix=".Dockerfile", delete=False
        ) as f:
            f.write(dockerfile_content.decode("utf-8"))
            dockerfile_path = f.name

        try:
            # Use buildx for multi-architecture builds or regular docker build
            if self.platform:
                multi_platform = "," in self.platform or self.platform == "all"
                cmd = [
                    "docker",
                    "buildx",
                    "build",
                    "-f",
                    dockerfile_path,
                    "-t",
                    tag,
                    "--platform",
                    self.platform,
                ]
                # For multi-arch builds, we need to push to registry
                # unless it's a dry run
                if not self.dry_run and multi_platform:
                    cmd.append("--push")
                else:
                    cmd.append("--load")
            else:
                cmd = [
                    "docker",
                    "buildx",
                    "build",
                    "-f",
                    dockerfile_path,
                    "-t",
                    tag,
                    "--load",  # Load image into docker instead of pushing to registry
                ]

            if annotations:
                print(f"Injecting annotations: {annotations}")
                for key, value in annotations.items():
                    cmd.extend(["--annotation", f"{key}={value}"])

            cmd.append(build_context)

            print(f"Running: {' '.join(cmd)}")
            if self.dry_run:
                print(
                    f"[dry-run] Building image {tag} with buildkit command line: {' '.join(cmd)}"
                )
                return True
            else:
                result = subprocess.run(cmd, capture_output=True, text=True)

                if result.returncode == 0:
                    print(f"Successfully built {tag} with Docker CLI")
                    return True
                else:
                    print(f"Docker CLI build failed for {tag}:")
                    print(f"STDOUT: {result.stdout}")
                    print(f"STDERR: {result.stderr}")
                    return False

        finally:
            # Clean up temporary Dockerfile
            try:
                os.unlink(dockerfile_path)
            except:
                pass

    def push_image(self, tag):
        print(f"Pushing image: {tag}")

        if self.dry_run:
            print(f"[dry-run] Would push image: {tag}")
            print(f"[dry-run] Command: docker push {tag}")
            return True

        try:
            # for local registry, we need to ensure the image is properly tagged
            if self.registry.startswith("localhost:"):
                # for local registry, push directly
                result = self.client.images.push(tag, stream=True, decode=True)

                for line in result:
                    if "status" in line:
                        print(f"  {line['status']}")
                    elif "error" in line:
                        print(f"  ERROR: {line['error']}")
                        return False

                print(f"Successfully pushed {tag}")
                return True
            else:
                # for remote registries, use standard push
                response = self.client.images.push(tag, stream=True)
                for line in response:
                    print(f"  {line}")
                print(f"Successfully pushed {tag}")
                return True

        except Exception as e:
            print(f"Failed to push {tag}: {e}")
            return False

    def export_image(self, tag, export_dir="."):
        """Export a Docker image to a tarball in the specified directory."""
        export_dir_path = Path(export_dir or ".").expanduser()
        print(f"Exporting image: {tag}")
        print(f"Export directory: {export_dir_path}")

        if self.dry_run:
            print(f"[dry-run] Would export image {tag} to {export_dir_path}")
            return True

        try:
            export_dir_path.mkdir(parents=True, exist_ok=True)
        except Exception as e:
            print(f"Failed to prepare export directory {export_dir_path}: {e}")
            return False

        safe_name = tag.replace("/", "_").replace(":", "_")
        export_path = export_dir_path / f"{safe_name}.tar"

        try:
            try:
                image = self.client.images.get(tag)
            except Exception:
                print(
                    f"Image {tag} not found locally. Attempting to pull from registry..."
                )
                image = self.client.images.pull(tag)

            with export_path.open("wb") as f:
                for chunk in image.save(named=True):
                    f.write(chunk)

            print(f"Image exported to {export_path}")
            return True
        except Exception as e:
            print(f"Failed to export image {tag}: {e}")
            return False

    def check_registry_access(self):
        import requests

        if self.dry_run:
            return True
        try:
            response = requests.get(f"http://{self.registry}/v2/")
            return response.status_code == 200
        except:
            return False

    def cleanup_images(self, tags):
        """Clean up images with given tags on build failure (not used for push failures)"""
        print("Cleaning up images due to build failure...")
        for tag in tags:
            try:
                self.client.images.remove(tag, force=True)
                print(f"  Removed: {tag}")
            except Exception as e:
                print(f"  Failed to remove {tag}: {e}")

    def interactive_build(self, no_push=False, dry_run=False, export_path=None):
        """Interactive build method with support for no_push and dry_run parameters"""
        print("=== Mica Image Builder ===")

        # Set dry_run mode if requested
        if dry_run:
            self.dry_run = True
            print(
                "[dry-run] Running in dry-run mode - no actual Docker operations will be performed"
            )

        # Setup registry unless in dry-run mode or explicitly no_push
        if not self.dry_run and not no_push:
            if not self.check_registry_access():
                print("Registry not accessible. Setting up...")
                if not self.setup_registry():
                    print("Failed to setup registry")
                    return False

        self.select_pedestal()
        self.select_os_type()
        self.select_os_versions()
        self.select_platform()
        self.select_image_files()

        # Get custom image description
        self.image_description = self.get_image_description()

        # Get image name (this also sets self.image_name)
        self.get_image_names()

        # Determine export behavior
        export_requested = False
        export_dir = None
        if export_path is not None:
            export_dir = export_path or "."
            export_requested = True
            print(f"Exporting final image tarball to: {Path(export_dir).expanduser()}")
        else:
            export_choice = (
                input("\nExport final image as tarball? (y/N): ").strip().lower()
            )
            if export_choice == "y":
                export_dir_input = input(
                    "Enter export directory (default: current directory): "
                ).strip()
                export_dir = export_dir_input or "."
                export_requested = True

        # Determine push behavior
        push_images = False
        if not self.dry_run and not no_push:
            # Ask if user wants to push to registry
            push_local = input("\nPush to local registry? (y/N): ").strip().lower()
            push_images = push_local == "y"

        # Use the unified build method
        success = self.unified_build(push_images=push_images)

        if not success:
            return False

        # Handle remote registry push (only if not in dry-run mode and we pushed locally)
        if not self.dry_run and push_images and not no_push:
            push_remote = input("\nPush to remote registry? (y/N): ").strip().lower()
            if push_remote == "y":
                remote_registry = input(
                    "Enter remote registry (e.g., registry.example.com): "
                ).strip()

                try:
                    local_final = self.image_name
                    local_repo = local_final
                    local_tag = "latest"
                    last_segment = local_final.rsplit("/", 1)[-1]
                    if ":" in last_segment:
                        local_repo, local_tag = local_final.rsplit(":", 1)

                    remote_repo_suffix = local_repo.split("/", 1)[-1]
                    remote_repo = f"{remote_registry}/{remote_repo_suffix}"

                    final_image = self.client.images.get(local_final)
                    final_image.tag(remote_repo, local_tag)
                    self.push_image(f"{remote_repo}:{local_tag}")
                except Exception as e:
                    print(f"Failed to push to remote registry: {e}")
                    return False

        if export_requested:
            if not self.export_image(self.image_name, export_dir or "."):
                return False

        return True

    def unified_build(self, push_images=False):
        """Unified build method that both interactive and CLI modes can use"""
        print(f"\n=== Building Mica {self.os_type} Container Images ===")

        built_images = []
        build_ctx = str(Path(__file__).absolute().parent.parent)

        # Display build configuration
        print(f"Pedestal: {self.pedestal}")
        print(f"OS: {self.os_type}")
        print(f"Firmware: {self.firmware_path}")
        if self.pedestal == "xen":
            print(f"Xen image: {self.xen_image_path}")
        print(f"Final image: {self.image_name}")
        if self.platform:
            print(f"Platform(s): {self.platform}")
        print(f"Dry run: {self.dry_run}")
        print(f"Push images: {push_images}")

        # Debug: Show the actual file paths that will be used
        print(f"\nDebug - Build context: {build_ctx}")
        print(f"Debug - Final firmware path in Dockerfile: {self.firmware_path}")
        if self.pedestal == "xen":
            print(f"Debug - Final Xen image path in Dockerfile: {self.xen_image_path}")

        print("\nBuilding final image...")
        final_dockerfile, final_labels, final_annotations = (
            self.generate_dockerfile_final()
        )

        if self.dry_run:
            print("[dry-run] Final Dockerfile preview below:")
            print(final_dockerfile.decode("utf-8"))
            print(f"[dry-run] Annotations to be applied: {final_annotations}")

        if not self.build_image_with_dockerfile(
            final_dockerfile,
            self.image_name,
            build_context=build_ctx,
            annotations=final_annotations,
        ):
            self.cleanup_images(built_images)
            return False
        built_images.append(self.image_name)

        # Push final image if requested
        if push_images:
            print("\nPushing final image...")
            if not self.push_image(self.image_name):
                print("Push failed, but keeping locally built image for debugging/manual push")
                print(f"Image preserved locally: {self.image_name}")
                return False

        print("\nBuild completed successfully!")
        print(f"Final image: {self.image_name}")
        if self.platform:
            print(f"Platform(s): {self.platform}")

        return True


def parse_arguments():
    parser = argparse.ArgumentParser(
        description="MicRan Image Builder - Build RTOS container images for MicRan",
        formatter_class=argparse.RawDescriptionHelpFormatter,
        epilog="""
Examples:
  # Interactive mode (no arguments) - includes platform selection
  python3 mica-image-builder.py

  # CLI mode with Xen pedestal and Zephyr OS
  python3 mica-image-builder.py --pedestal xen --os zephyr \
    --firmware bundle.sample/zephyr.xen.elf --xen-image bundle.sample/zephyr.xen.bin

  # CLI mode with custom image name and version
  python3 mica-image-builder.py --pedestal openamp --os zephyr \
    --firmware firmware.elf --image-name localhost:5000/mica-zephyr-myapp:openamp-1.0

  # Build and push to registry
  python3 mica-image-builder.py --pedestal xen --os zephyr \
    --firmware firmware.elf --xen-image xen.bin --push

  # CLI mode with custom version
  python3 mica-image-builder.py --pedestal xen --os zephyr \\
    --firmware firmware.elf --xen-image xen.bin --version 2.0

  # Export final image tarball to ./exports
  python3 mica-image-builder.py --pedestal xen --os zephyr \\
    --firmware firmware.elf --xen-image xen.bin --export ./exports

  # Multi-architecture build for amd64 and arm64
  python3 mica-image-builder.py --pedestal xen --os zephyr \\
    --firmware firmware.elf --xen-image xen.bin --platform linux/amd64,linux/arm64

  # Multi-architecture build for all supported platforms
  python3 mica-image-builder.py --pedestal xen --os zephyr \\
    --firmware firmware.elf --xen-image xen.bin --platform all --push

  # Interactive mode platform selection options:
  # 1. Native build (default) - builds for host architecture
  # 2. amd64 only - builds for linux/amd64 only
  # 3. arm64 only - builds for linux/arm64 only
  # 4. amd64 + arm64 - builds for both platforms
  # 5. All platforms - builds for all supported platforms

  # Interactive mode with dry-run (preview commands)
  python3 mica-image-builder.py --dry-run

  # Interactive mode without pushing to registry
  python3 mica-image-builder.py --no-push

  # Interactive mode with both dry-run and no-push
  python3 mica-image-builder.py --dry-run --no-push
""",
    )

    # Build parameters (any of these enables CLI mode)
    parser.add_argument(
        "--pedestal",
        choices=["xen", "openamp"],
        help="Pedestal type (xen or openamp) - enables CLI mode",
    )
    parser.add_argument(
        "--os",
        choices=["zephyr", "uniproton"],
        help="OS type (zephyr or uniproton) - enables CLI mode",
    )
    parser.add_argument(
        "--firmware", help="Path to firmware file (ELF/BIN) - enables CLI mode"
    )
    parser.add_argument(
        "--xen-image",
        help="Path to Xen image file (required for xen pedestal) - enables CLI mode",
    )
    parser.add_argument(
        "--image-name",
        help="Final image name (default: registry/mica-{os}-{app}:{pedestal}-{version}) - enables CLI mode",
    )
    parser.add_argument(
        "--registry",
        default="localhost:5000",
        help="Registry address (default: localhost:5000) - enables CLI mode",
    )
    parser.add_argument(
        "--version",
        default="0.1",
        help="Image version (default: 0.1) - enables CLI mode",
    )
    parser.add_argument(
        "--push",
        action="store_true",
        help="Push images to registry after building - enables CLI mode",
    )
    parser.add_argument(
        "--no-push",
        action="store_true",
        help="Skip pushing images to registry (for interactive mode)",
    )
    parser.add_argument(
        "--dry-run",
        action="store_true",
        help="Generate Dockerfiles and print actions without invoking Docker (works with both interactive and CLI modes)",
    )
    parser.add_argument(
        "--export",
        nargs="?",
        const=".",
        default=None,
        help="Export the final image as a tarball. Optionally provide the target directory (defaults to current directory).",
    )
    parser.add_argument(
        "--platform",
        help="Target platform(s) for multi-architecture builds (e.g., linux/amd64,linux/arm64 or all)",
    )
    parser.add_argument(
        "--builder", help="Docker buildx builder name (default: container-builder)"
    )

    return parser.parse_args()


def should_use_cli_mode(args):
    """Check if any CLI arguments are provided (excluding --no-push and --dry-run which work with interactive mode)"""
    return any(
        [
            args.pedestal,
            args.os,
            args.firmware,
            args.xen_image,
            args.image_name,
            args.registry != "localhost:5000",
            args.version != "0.1",
            args.push,
            args.platform,
            args.builder,
        ]
    )


def cli_build(builder, args):
    """Build images using CLI arguments with shared unified build logic"""
    print("=== Mica Image Builder (CLI Mode) ===")

    # Change to scripts directory for proper build context
    import os

    scripts_dir = Path(__file__).absolute().parent.parent
    original_cwd = os.getcwd()
    os.chdir(scripts_dir)
    print(f"Changed working directory to: {scripts_dir}")

    try:
        # Validate required CLI arguments
        if not args.pedestal:
            print("Error: --pedestal is required when using CLI mode")
            return False
        if not args.os:
            print("Error: --os is required when using CLI mode")
            return False
        if not args.firmware:
            print("Error: --firmware is required when using CLI mode")
            return False
        if args.pedestal == "xen" and not args.xen_image:
            print("Error: --xen-image is required for xen pedestal")
            return False

        # Set builder properties from CLI arguments
        builder.registry = args.registry
        builder.pedestal = args.pedestal
        builder.os_type = args.os
        builder.platform = builder.resolve_platforms(args.platform)

        # Convert file paths to relative paths from scripts directory
        scripts_dir = Path(__file__).absolute().parent.parent

        # Handle firmware path
        firmware_path_obj = Path(args.firmware)
        if firmware_path_obj.is_absolute():
            try:
                builder.firmware_path = str(firmware_path_obj.relative_to(scripts_dir))
            except ValueError:
                # If file is not under scripts directory, keep absolute path
                builder.firmware_path = args.firmware
        else:
            builder.firmware_path = args.firmware

        # Handle Xen image path if provided
        if args.xen_image:
            xen_path_obj = Path(args.xen_image)
            if xen_path_obj.is_absolute():
                try:
                    builder.xen_image_path = str(xen_path_obj.relative_to(scripts_dir))
                except ValueError:
                    # If file is not under scripts directory, keep absolute path
                    builder.xen_image_path = args.xen_image
            else:
                builder.xen_image_path = args.xen_image

        # Set image name
        if args.image_name:
            builder.image_name = args.image_name
        else:
            # Default format: {registry}/mica-{os}-app:{pedestal}-{version}
            builder.image_name = f"{builder.registry}/mica-{builder.os_type}-app:{builder.pedestal}-{args.version}"

        # Setup registry if needed (unless in dry-run mode)
        if args.dry_run:
            print("[dry-run] Skipping registry checks and setup.")
        elif not builder.check_registry_access():
            print("Registry not accessible. Setting up...")
            if not builder.setup_registry():
                print("Failed to setup registry")
                return False

        # Use the unified build method
        success = builder.unified_build(push_images=args.push)

        if success and args.export is not None:
            export_dir = args.export or "."
            if not builder.export_image(builder.image_name, export_dir):
                return False

        return success

    finally:
        # Restore original working directory
        os.chdir(original_cwd)


if __name__ == "__main__":
    args = parse_arguments()
    builder = MicaImageBuilder(init_docker=not args.dry_run)

    try:
        if should_use_cli_mode(args):
            success = cli_build(builder, args)
        else:
            # Interactive mode - pass no_push and dry_run arguments if provided
            success = builder.interactive_build(
                no_push=args.no_push, dry_run=args.dry_run, export_path=args.export
            )
        sys.exit(0 if success else 1)
    except KeyboardInterrupt:
        print("\nBuild cancelled by user")
        sys.exit(1)
