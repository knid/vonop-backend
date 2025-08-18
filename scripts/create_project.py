import pathlib

from jinja2 import Template

BASE_PATH                  = pathlib.Path(__file__).resolve().parent.parent
WEMOB_MOBILE_TEMPLATE_PATH = BASE_PATH / "wemob_mobile_template"
WEMOB_PROJECTS_PATH        = BASE_PATH / "projects"

WEMOB_TEMPLATE_FILES = [
    "pubspec.yaml",
    "lib/main.dart",
]


def create_project(app_id: str, app_name: str):
    pass