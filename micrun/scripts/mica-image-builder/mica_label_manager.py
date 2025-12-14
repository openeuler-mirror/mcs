#!/usr/bin/env python3

# Prefer stdlib tomllib (Python 3.11+), fall back to tomli for older Pythons.
try:
    import tomllib  # type: ignore
except Exception:  # pragma: no cover
    import tomli as tomllib  # type: ignore
from datetime import datetime
from pathlib import Path

class MicaLabelManager:
    def __init__(self, config_path="mica-labels.toml"):
        # Resolve default config relative to this file to be robust to CWD.
        self.micran_annotation_prefix = "org.openeuler.micran"
        self.runtime_label_prefix = "org.opencontainers.image"
        default_path = Path(__file__).absolute().parent / "mica-labels.toml"
        self.config_path = Path(config_path)
        if config_path == "mica-labels.toml":
            self.config_path = default_path
        self.labels_config = self._load_config()

    def _load_config(self):
        if not self.config_path.exists():
            raise FileNotFoundError(f"Label config not found: {self.config_path}")

        with open(self.config_path, 'rb') as f:
            return tomllib.load(f)

    def generate_labels_and_annotations(self, pedestal=None, os_type=None, **kwargs):
        labels = {}
        annotations = {}

        # Add base labels (runtime labels - go into Dockerfile LABEL)
        if 'base' in self.labels_config:
            for key, value in self.labels_config['base'].items():
                rendered_value = self._render_template(value, kwargs)
                if rendered_value is not None:
                    labels[f"{self.runtime_label_prefix}.{key}"] = rendered_value

        # Add pedestal-specific annotations (build-time metadata)
        if pedestal and 'pedestal' in self.labels_config and pedestal in self.labels_config['pedestal']:
            pedestal_config = self.labels_config['pedestal'][pedestal]
            for key, value in pedestal_config.items():
                rendered_value = self._render_template(value, kwargs)
                if rendered_value is not None:
                    annotations[f"{self.micran_annotation_prefix}.ped.{key}"] = rendered_value

        # Add OS-specific annotations (build-time metadata)
        if os_type and 'os' in self.labels_config and os_type in self.labels_config['os']:
            os_config = self.labels_config['os'][os_type]
            for key, value in os_config.items():
                rendered_value = self._render_template(value, kwargs)
                if rendered_value is not None:
                    annotations[f"{self.micran_annotation_prefix}.container.{key}"] = rendered_value

        # Add compatibility annotations (build-time metadata)
        if os_type and 'compatibility' in self.labels_config and os_type in self.labels_config['compatibility']:
            for key, value in self.labels_config['compatibility'][os_type].items():
                rendered_value = self._render_template(value, kwargs)
                if rendered_value is not None:
                    annotations[f"{self.micran_annotation_prefix}.compatibility.{key}"] = rendered_value
        elif 'default-compatibility' in self.labels_config:
            # Fallback to default compatibility if OS-specific doesn't exist
            for key, value in self.labels_config['default-compatibility'].items():
                rendered_value = self._render_template(value, kwargs)
                if rendered_value is not None:
                    annotations[f"{self.micran_annotation_prefix}.compatibility.{key}"] = rendered_value

        # Add custom annotations from non-default sections (excluding default- sections)
        for section_name, section_config in self.labels_config.items():
            if not section_name.startswith('default-') and section_name not in ['base', 'pedestal', 'os', 'compatibility']:
                # This is a custom extension section
                for key, value in section_config.items():
                    rendered_value = self._render_template(value, kwargs)
                    if rendered_value is not None:
                        annotations[f"{self.micran_annotation_prefix}.{section_name}.{key}"] = rendered_value

        # Ensure critical MicRan annotations are always present
        if pedestal:
            annotations[f"{self.micran_annotation_prefix}.ped.pedestal"] = pedestal
        if os_type:
            annotations[f"{self.micran_annotation_prefix}.container.os"] = os_type

        return labels, annotations

    def _render_template(self, template, context):
        # Handle complex data structures (dicts, lists, etc.)
        if isinstance(template, dict):
            rendered_dict = {}
            for key, value in template.items():
                rendered_value = self._render_template(value, context)
                if rendered_value is not None:
                    rendered_dict[key] = rendered_value
            return rendered_dict if rendered_dict else None
        elif isinstance(template, list):
            rendered_list = []
            for item in template:
                rendered_item = self._render_template(item, context)
                if rendered_item is not None:
                    rendered_list.append(rendered_item)
            return rendered_list if rendered_list else None
        elif not isinstance(template, str):
            return template

        context['timestamp'] = datetime.now().isoformat()

        for key, value in context.items():
            placeholder = f"{{{{{key}}}}}"
            if placeholder in template:
                template = template.replace(placeholder, str(value))

        # Check if there are any unresolved placeholders remaining
        if '{{' in template and '}}' in template:
            return None  # Skip this label as it has unresolved placeholders

        return template

    def format_docker_labels(self, labels):
        """Generate LABEL directives for Dockerfile (runtime labels only)"""
        dockerfile_lines = []
        for key, value in labels.items():
            if value is not None:
                dockerfile_lines.append(f'LABEL {key}="{value}"')
        return '\n'.join(dockerfile_lines) if dockerfile_lines else ""

    def get_scratch_labels_and_annotations(self, pedestal, os_type):
        return self.generate_labels_and_annotations(
            pedestal=pedestal,
            os_type=os_type,
            image_type="scratch"
        )

    def get_final_labels_and_annotations(self, pedestal, os_type, xen_image_path=None, firmware_path="/firmware.elf", custom_description=None, zephyr_version="3.7.1", uniproton_version="latest"):
        # The xen_image_path parameter is kept for API compatibility but not used
        # because the annotation should contain the container path (/image.bin),
        # not the source path from build context
        context = {
            "image_type": "application",
            "xen_image_path": "/image.bin",
            "firmware_path": firmware_path,
            "description": custom_description or f"Mica {os_type} Container Image",
            "zephyr_version": zephyr_version,
            "uniproton_version": uniproton_version
        }
        return self.generate_labels_and_annotations(
            pedestal=pedestal,
            os_type=os_type,
            **context
        )

    # Backward compatibility methods
    def get_scratch_labels(self, pedestal, os_type):
        """Legacy method - returns all labels for backward compatibility"""
        labels, annotations = self.get_scratch_labels_and_annotations(pedestal, os_type)
        # Combine for backward compatibility
        return {**labels, **annotations}

    def get_final_labels(self, pedestal, os_type, xen_image_path=None, firmware_path="/firmware.elf", custom_description=None, zephyr_version="3.7.1", uniproton_version="latest"):
        """Legacy method - returns all labels for backward compatibility"""
        labels, annotations = self.get_final_labels_and_annotations(
            pedestal, os_type, xen_image_path, firmware_path,
            custom_description, zephyr_version, uniproton_version
        )
        # Combine for backward compatibility
        return {**labels, **annotations}
