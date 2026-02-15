#!/usr/bin/env python3
"""
TDTP Framework - Project Cleanup Script
========================================

Removes temporary files, old configs, and cleans up the project.

Usage:
    python scripts/cleanup.py [--dry-run]

Options:
    --dry-run    Show what would be deleted without actually deleting
"""

import os
import sys
import shutil
import subprocess
import argparse
from pathlib import Path
from typing import List, Tuple


class ProjectCleaner:
    def __init__(self, project_root: Path, dry_run: bool = False):
        self.project_root = project_root
        self.dry_run = dry_run
        self.removed_count = 0
        self.removed_size = 0

    def get_size(self, path: Path) -> int:
        """Get size of file or directory in bytes"""
        if path.is_file():
            return path.stat().st_size
        elif path.is_dir():
            return sum(f.stat().st_size for f in path.rglob('*') if f.is_file())
        return 0

    def remove_path(self, path: Path, reason: str):
        """Remove file or directory with logging"""
        if not path.exists():
            return

        size = self.get_size(path)
        size_mb = size / (1024 * 1024)

        if self.dry_run:
            print(f"[DRY-RUN] Would remove: {path.relative_to(self.project_root)} ({size_mb:.2f} MB) - {reason}")
        else:
            print(f"Removing: {path.relative_to(self.project_root)} ({size_mb:.2f} MB) - {reason}")
            if path.is_dir():
                shutil.rmtree(path)
            else:
                path.unlink()

        self.removed_count += 1
        self.removed_size += size

    def clean_temp_files(self):
        """Remove temporary files and build artifacts"""
        print("\n=== Cleaning Temporary Files ===")

        temp_patterns = [
            # Build artifacts
            ('**/dist', 'Build artifact'),
            ('**/build', 'Build artifact'),
            ('**/__pycache__', 'Python cache'),
            ('**/*.pyc', 'Python bytecode'),
            ('**/.pytest_cache', 'Pytest cache'),
            ('**/*.egg-info', 'Python egg info'),

            # IDE
            ('**/.vscode', 'VS Code settings'),
            ('**/.idea', 'IntelliJ settings'),

            # OS
            ('**/.DS_Store', 'macOS metadata'),
            ('**/Thumbs.db', 'Windows thumbnails'),

            # Temporary
            ('**/*.tmp', 'Temporary file'),
            ('**/*.temp', 'Temporary file'),
            ('**/*.log', 'Log file'),
            ('**/*.bak', 'Backup file'),
        ]

        for pattern, reason in temp_patterns:
            for path in self.project_root.glob(pattern):
                # Skip .git directory
                if '.git' in path.parts:
                    continue
                self.remove_path(path, reason)

    def clean_old_docker_configs(self):
        """Remove conflicting docker-compose files"""
        print("\n=== Cleaning Old Docker Configs ===")

        # Keep only the main docker-compose.yml
        # Remove others that might conflict
        old_configs = [
            'cmd/tdtp-xray/docker-compose.yml',  # Conflicts with main config
            'tests/integration/docker-compose.yml',  # Superseded by main
        ]

        for config_path in old_configs:
            path = self.project_root / config_path
            if path.exists():
                self.remove_path(path, 'Superseded by main docker-compose.yml')

    def clean_old_frontend_dist(self):
        """Ensure no old dist/ directories exist in frontend"""
        print("\n=== Cleaning Old Frontend Builds ===")

        # We already removed frontend/dist/ but check again
        dist_paths = [
            'cmd/tdtp-xray/frontend/dist',
        ]

        for dist_path in dist_paths:
            path = self.project_root / dist_path
            if path.exists():
                self.remove_path(path, 'Old frontend build (using src/ now)')

    def clean_test_databases(self):
        """Remove test SQLite databases"""
        print("\n=== Cleaning Test Databases ===")

        for db_file in self.project_root.rglob('*.db'):
            if '.git' not in db_file.parts:
                self.remove_path(db_file, 'Test database')

        for db_file in self.project_root.rglob('*.sqlite'):
            if '.git' not in db_file.parts:
                self.remove_path(db_file, 'Test database')

    def stop_docker_containers(self):
        """Stop and remove old Docker containers"""
        print("\n=== Stopping Old Docker Containers ===")

        if self.dry_run:
            print("[DRY-RUN] Would stop and remove all Docker containers")
            return

        try:
            # List all containers
            result = subprocess.run(
                ['docker', 'ps', '-aq'],
                capture_output=True,
                text=True,
                check=False
            )

            if result.returncode == 0 and result.stdout.strip():
                container_ids = result.stdout.strip().split('\n')
                print(f"Found {len(container_ids)} containers to remove")

                # Stop all
                subprocess.run(['docker', 'stop'] + container_ids, check=False)
                # Remove all
                subprocess.run(['docker', 'rm'] + container_ids, check=False)

                print("✓ All Docker containers stopped and removed")
            else:
                print("No Docker containers found")

        except FileNotFoundError:
            print("⚠ Docker not found - skipping container cleanup")
        except Exception as e:
            print(f"⚠ Error cleaning Docker containers: {e}")

    def run(self, clean_docker: bool = False):
        """Run all cleanup tasks"""
        print(f"\n{'='*60}")
        print(f"TDTP Framework Project Cleanup")
        print(f"Project: {self.project_root}")
        print(f"Mode: {'DRY-RUN' if self.dry_run else 'LIVE'}")
        print(f"{'='*60}")

        self.clean_temp_files()
        self.clean_old_docker_configs()
        self.clean_old_frontend_dist()
        self.clean_test_databases()

        if clean_docker:
            self.stop_docker_containers()

        # Summary
        size_mb = self.removed_size / (1024 * 1024)
        print(f"\n{'='*60}")
        print(f"Summary:")
        print(f"  Items removed: {self.removed_count}")
        print(f"  Space freed: {size_mb:.2f} MB")
        if self.dry_run:
            print(f"  Mode: DRY-RUN (nothing was actually deleted)")
        print(f"{'='*60}\n")


def main():
    parser = argparse.ArgumentParser(description='Clean TDTP Framework project')
    parser.add_argument('--dry-run', action='store_true', help='Show what would be deleted')
    parser.add_argument('--docker', action='store_true', help='Also clean Docker containers')
    args = parser.parse_args()

    # Find project root (where this script is located)
    script_dir = Path(__file__).parent
    project_root = script_dir.parent

    cleaner = ProjectCleaner(project_root, dry_run=args.dry_run)
    cleaner.run(clean_docker=args.docker)

    if args.dry_run:
        print("Run without --dry-run to actually delete files")
    else:
        print("✓ Cleanup complete!")


if __name__ == '__main__':
    main()
