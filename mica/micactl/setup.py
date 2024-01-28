from pathlib import Path
from setuptools import setup
import re

setup_dir = Path(__file__).resolve().parent
version = re.search(
    r'__version__ = "(.*)"',
    Path(setup_dir, 'mica.py').open().read()
)
if version is None:
    raise SystemExit("Could not determine version to use")
version = version.group(1)

setup(
    name='mica',
    url='https://gitee.com/openeuler/mcs',
    description='command line client for mica',
    license='MulanPSL-2.0',
    py_modules=['mica'],
    entry_points={
        "console_scripts": [
            "mica = mica:main"
        ]
    },
    version=version,
    python_requires='~=3.8',
)
