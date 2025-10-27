import argparse
import pathlib
import shutil
import os
import zipfile
import psycopg2
import json
import subprocess
import secrets
import string
from jinja2 import Template

BASE_PATH                  = pathlib.Path(__file__).resolve().parent.parent
WEMOB_MOBILE_TEMPLATE_PATH = BASE_PATH / "mobile/template"
WEMOB_PROJECTS_PATH        = BASE_PATH / "projects"

WEMOB_TEMPLATE_FILES = [
    "pubspec.yaml",
    "lib/main.dart",
]

# Postgres connection settings sourced from environment (POSTGRES_* prefix)
POSTGRES_URL      = os.environ.get("DB_URL")  # optional full DSN
POSTGRES_HOST     = os.environ.get("DB_HOST", "localhost")
POSTGRES_PORT     = os.environ.get("DB_PORT", "5432")
POSTGRES_DB       = os.environ.get("DB_NAME")
POSTGRES_USER     = os.environ.get("DB_USER")
POSTGRES_PASSWORD = os.environ.get("DB_PASSWORD")
POSTGRES_SSLMODE  = os.environ.get("DB_SSLMODE", "disable")  # e.g., require, disable

# Android signing credentials (required when generating a dummy keystore)
ANDROID_KEYSTORE_STORE_PASS = os.environ.get("ANDROID_KEYSTORE_STORE_PASS")
ANDROID_KEYSTORE_KEY_PASS   = os.environ.get("ANDROID_KEYSTORE_KEY_PASS")
ANDROID_KEYSTORE_ORG        = "Wemob"


def _generate_random_password(length: int = 32) -> str:
    alphabet = string.ascii_letters + string.digits
    return "".join(secrets.choice(alphabet) for _ in range(length))


def _read_key_properties(path: pathlib.Path):
    if not path.exists():
        return {}

    properties = {}
    with open(path, "r", encoding="utf-8") as props_file:
        for raw_line in props_file:
            line = raw_line.strip()
            if not line or line.startswith("#") or "=" not in line:
                continue
            key, value = line.split("=", 1)
            properties[key.strip()] = value.strip()
    return properties

def parse_args():
    parser = argparse.ArgumentParser(description="Create a Wemob build project.")
    parser.add_argument("--project-id", required=True, help="Project ID")
    parser.add_argument("--build-task-id", required=True, help="Build task ID")
    parser.add_argument("--onesignal-app-id", required=True, help="Onesignal app ID")
    parser.add_argument("--build-id", required=True, help="Build ID")
    parser.add_argument("--title", required=True, help="Application title")
    parser.add_argument("--url", required=True, help="URL")
    parser.add_argument("--icon", required=True, help="Icon path or URL")
    parser.add_argument("--sscolor", required=True, help="Splash screen color")
    parser.add_argument("--package-name", required=True, help="Package name")
    parser.add_argument("--build-android", action="store_true", help="Build for Android")
    parser.add_argument("--remove-wemob-branding", action="store_true", help="Remove Wemob branding")
    return parser.parse_args()

def render_templates(task_id, app_id, onesignal_app_id, url, icon, sscolor, remove_wemob_branding):
    """
    Compute base template folder as WEMOB_PROJECTS_PATH/{build_id} and update
    template files in-place there, keeping other files unchanged.

    Note: "project_dir" is not used for writing anymore; kept for signature
    compatibility with callers.
    """
    base_template_dir = WEMOB_PROJECTS_PATH / app_id
    
    # Copy WEMOB_MOBILE_TEMPLATE_PATH to WEMOB_PROJECTS_PATH/{app_id}
    if base_template_dir.exists():
        shutil.rmtree(base_template_dir)
    shutil.copytree(WEMOB_MOBILE_TEMPLATE_PATH, base_template_dir, symlinks=True, dirs_exist_ok=True)

    for template_file in WEMOB_TEMPLATE_FILES:
        template_path = base_template_dir / template_file
        # Read existing template content from the base template folder
        with open(template_path, "r") as f:
            template_content = f.read()

        template = Template(template_content)
        rendered_content = template.render(
            WEMOB_PROJECT_ID=app_id,
            ONESIGNAL_APP_ID=onesignal_app_id,
            WEMOB_PROJECT_URL=url,
            WEMOB_PROJECT_ICON=icon,
            WEMOB_SPLASH_SCREEN_COLOR=sscolor,
            REMOVE_WEMOB_BRANDING=remove_wemob_branding,
        )

        with open(template_path, "w") as f:
            f.write(rendered_content)


def zip_project(project_dir, app_id):
    zip_path = WEMOB_PROJECTS_PATH / f"{app_id}.zip"
    with zipfile.ZipFile(zip_path, 'w', zipfile.ZIP_DEFLATED) as zipf:
        for root, dirs, files in os.walk(project_dir):
            for file in files:
                file_path = pathlib.Path(root) / file
                arcname = file_path.relative_to(project_dir)
                zipf.write(file_path, arcname)
    return zip_path


def create_dummy_aab(app_id, app_name, build_id):
    project_dir = WEMOB_PROJECTS_PATH / app_id
    android_dir = project_dir / "android"
    android_app_dir = android_dir / "app"
    keystore_path = android_app_dir / "upload-keystore.jks"
    key_properties_path = android_dir / "key.properties"

    existing_props = _read_key_properties(key_properties_path)
    store_pass = ANDROID_KEYSTORE_STORE_PASS or existing_props.get("storePassword")
    key_pass = ANDROID_KEYSTORE_KEY_PASS or existing_props.get("keyPassword")

    if not keystore_path.exists():
        android_app_dir.mkdir(parents=True, exist_ok=True)

        store_pass = store_pass or _generate_random_password()
        key_pass = key_pass or _generate_random_password()

        tenant_name = app_name or app_id
        distinguished_name = (
            f"CN={tenant_name}, OU=Mobile, O={ANDROID_KEYSTORE_ORG}, L=London, ST=London, C=GB"
        )

        keytool_command = [
            "keytool",
            "-genkeypair",
            "-v",
            "-keystore",
            str(keystore_path),
            "-storetype",
            "JKS",
            "-keyalg",
            "RSA",
            "-keysize",
            "2048",
            "-validity",
            "10000",
            "-alias",
            "upload",
            "-dname",
            distinguished_name,
            "-storepass",
            store_pass,
            "-keypass",
            key_pass,
        ]

        try:
            subprocess.run(keytool_command, check=True, cwd=str(android_app_dir))
        except FileNotFoundError as exc:
            raise RuntimeError(
                "keytool executable not found; ensure the JDK is installed and keytool is on PATH"
            ) from exc
        except subprocess.CalledProcessError as exc:
            raise RuntimeError(
                f"Failed to generate upload keystore for {app_id}: {exc}"
            ) from exc
    else:
        if not store_pass or not key_pass:
            raise RuntimeError( 
                "Existing Android keystore found but credentials are missing; provide them via key.properties or environment variables"
            )

    android_dir.mkdir(parents=True, exist_ok=True)
    key_properties_content = (
        f"storePassword={store_pass}\n"
        f"keyPassword={key_pass}\n"
        "keyAlias=upload\n"
        "storeFile=upload-keystore.jks\n"
    )
    with open(key_properties_path, "w", encoding="utf-8") as props_file:
        props_file.write(key_properties_content)

    flutter_command = [
        "flutter",
        "build",
        "appbundle",
        "--release",
    ]

    try:
        subprocess.run(flutter_command, check=True, cwd=str(project_dir))
    except FileNotFoundError as exc:
        raise RuntimeError(
            "flutter executable not found; ensure Flutter SDK is installed and flutter is on PATH"
        ) from exc
    except subprocess.CalledProcessError as exc:
        raise RuntimeError(
            f"Flutter build appbundle failed for {app_id}: {exc}"
        ) from exc

    built_aab_path = project_dir / "build" / "app" / "outputs" / "bundle" / "release" / "app-release.aab"
    if not built_aab_path.exists():
        raise RuntimeError(
            f"Expected AAB not found at {built_aab_path}; check Flutter build output"
        )

    aab_path = BASE_PATH / "storage" / "public" / "builds" / f"{build_id}.aab"
    aab_path.parent.mkdir(parents=True, exist_ok=True)
    shutil.copy2(built_aab_path, aab_path)
    return aab_path


def _pg_connect():
    """Create a psycopg2 connection using POSTGRES_* environment variables."""
    if POSTGRES_URL:
        return psycopg2.connect(POSTGRES_URL)

    if not POSTGRES_DB or not POSTGRES_USER:
        raise RuntimeError(
            "Database configuration missing: set POSTGRES_DB and POSTGRES_USER or POSTGRES_URL"
        )

    params = {
        "host": POSTGRES_HOST,
        "port": POSTGRES_PORT,
        "dbname": POSTGRES_DB,
        "user": POSTGRES_USER,
        "password": POSTGRES_PASSWORD,
    }
    if POSTGRES_SSLMODE:
        params["sslmode"] = POSTGRES_SSLMODE

    return psycopg2.connect(**params)


def update_db(task_id):
    conn = _pg_connect()
    try:
        with conn:
            with conn.cursor() as cur:
                cur.execute("UPDATE app_build_tasks SET finished = true, status_text = 'Builded', execute_time = NOW() WHERE id = %s", (task_id,))
    finally:
        conn.close()


def create_project(task_id: str, build_id: str, app_id: str, app_name: str, url: str, icon: str, sscolor: str, build_android: bool, onesignal_app_id: str, package_name: str, remove_wemob_branding: bool):
    render_templates(task_id, app_id, onesignal_app_id, url, icon, sscolor, remove_wemob_branding)
    update_package_name(app_id, package_name)
    change_icon(app_id, icon)
    aab_path = create_dummy_aab(app_id, app_name, build_id) if build_android else None
    update_db(task_id)

def update_package_name(app_id: str, package_name: str):
    project_dir = WEMOB_PROJECTS_PATH / app_id

    command = [
        "dart",
        "run",
        "change_app_package_name:main",
        package_name,
    ]    
        
    subprocess.run(command, check=True, cwd=str(project_dir))

def change_icon(app_id: str, icon: str):
    project_dir = WEMOB_PROJECTS_PATH / app_id
    
    assets_icon_dir = project_dir / "assets" / "icon"
    assets_icon_dir.mkdir(parents=True, exist_ok=True)
    dest_icon_path = assets_icon_dir / "app_icon.png"
    shutil.copy2(BASE_PATH / "storage" / "public" / "icons" / icon, dest_icon_path)

    command = [
        "dart",
        "run",
        "flutter_launcher_icons",
    ]    
        
    subprocess.run(command, check=True, cwd=str(project_dir))

def main():
    print("********Creating project...*********")
    args = parse_args()
    create_project(
        task_id=args.build_task_id,
        build_id=args.build_id,
        app_id=args.project_id,
        app_name=args.title,
        url=args.url,
        icon=args.icon,
        sscolor=args.sscolor,
        build_android=args.build_android,
        onesignal_app_id=args.onesignal_app_id,
        package_name=args.package_name,
        remove_wemob_branding=args.remove_wemob_branding,
    )
    
if __name__ == "__main__":
    main()
