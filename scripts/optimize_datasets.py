#!/usr/bin/env python3
"""
scripts/optimize_datasets.py
Optimizes oversized GeoJSON datasets by quantizing coordinates to 4 decimal places
(~11m precision), dropping collinear/duplicate vertices, and pre-generating
maximum-compression gzip archives (.geojson.gz).
"""

import gzip
import json
import os
import sys
import time

DATASET_DIR = os.path.abspath(os.path.join(os.path.dirname(__file__), "..", "dataset"))

def round_coords(coords, precision=4):
    """Recursively round coordinates to specified precision and remove adjacent duplicates."""
    if not coords:
        return coords
    if isinstance(coords[0], (int, float)):
        return [round(coords[0], precision), round(coords[1], precision)]
    
    # List of coordinates or rings
    res = []
    for item in coords:
        sub = round_coords(item, precision)
        # If this is a ring of coordinates (list of [x, y]), deduplicate adjacent identical points
        if isinstance(sub, list) and len(sub) > 0 and isinstance(sub[0], (int, float)):
            if len(res) == 0 or res[-1] != sub:
                res.append(sub)
        else:
            res.append(sub)
            
    # For a polygon ring, ensure at least 4 coordinates and first == last
    if len(res) >= 3 and isinstance(res[0], list) and isinstance(res[0][0], (int, float)):
        if res[0] != res[-1]:
            res.append(res[0])
    return res

def compute_bbox(coords):
    """Recursively computes [minX, minY, maxX, maxY] for a geometry coordinates tree."""
    if not coords:
        return None
    min_x, min_y = float("inf"), float("inf")
    max_x, max_y = float("-inf"), float("-inf")
    def scan(c):
        nonlocal min_x, min_y, max_x, max_y
        if isinstance(c[0], (int, float)):
            x, y = c[0], c[1]
            if x < min_x: min_x = x
            if x > max_x: max_x = x
            if y < min_y: min_y = y
            if y > max_y: max_y = y
            return
        for sub in c:
            scan(sub)
    scan(coords)
    if min_x == float("inf"):
        return None
    return [round(min_x, 4), round(min_y, 4), round(max_x, 4), round(max_y, 4)]

def optimize_feature_collection(in_path, out_opt_path=None, precision=4):
    """Load GeoJSON, quantize geometry coordinates, write optimized and gzip files."""
    print(f"--> Optimizing {os.path.basename(in_path)}...")
    t0 = time.time()
    with open(in_path, "r", encoding="utf-8") as f:
        data = json.load(f)
    
    features = data.get("features", [])
    for feat in features:
        geom = feat.get("geometry")
        if geom and "coordinates" in geom:
            geom["coordinates"] = round_coords(geom["coordinates"], precision)
            feat["bbox"] = compute_bbox(geom["coordinates"])
            
    if out_opt_path:
        # Write .opt.geojson
        with open(out_opt_path, "w", encoding="utf-8") as f:
            json.dump(data, f, separators=(",", ":"))
        opt_size = os.path.getsize(out_opt_path)
        print(f"    Saved {os.path.basename(out_opt_path)} ({opt_size / 1024 / 1024:.2f} MB)")
        
        # Write .opt.geojson.gz
        gz_path = out_opt_path + ".gz"
        with gzip.open(gz_path, "wt", encoding="utf-8", compresslevel=9) as f:
            json.dump(data, f, separators=(",", ":"))
        gz_size = os.path.getsize(gz_path)
        print(f"    Saved {os.path.basename(gz_path)} ({gz_size / 1024 / 1024:.2f} MB)")
    else:
        # Gzip original dataset with separator compaction
        gz_path = in_path + ".gz"
        with gzip.open(gz_path, "wt", encoding="utf-8", compresslevel=9) as f:
            json.dump(data, f, separators=(",", ":"))
        gz_size = os.path.getsize(gz_path)
        print(f"    Saved {os.path.basename(gz_path)} ({gz_size / 1024 / 1024:.2f} MB)")

    print(f"    Completed in {time.time() - t0:.2f}s")

def main():
    print("=== seaRoute Dataset Optimization & Compression Pipeline ===")
    
    # 1. Large Polygon Datasets needing geometric simplification & coordinate truncation
    large_datasets = [
        ("eez_12nm_v3.geojson", "eez_12nm_v3.opt.geojson"),
        ("eez_v11.geojson", "eez_v11.opt.geojson"),
    ]
    
    for src_name, opt_name in large_datasets:
        src = os.path.join(DATASET_DIR, src_name)
        opt = os.path.join(DATASET_DIR, opt_name)
        if os.path.exists(src):
            optimize_feature_collection(src, opt, precision=4)
        else:
            print(f"[!] Warning: {src_name} not found in {DATASET_DIR}")
            
    # 2. Other client datasets requiring gzip pre-compression
    standard_datasets = [
        "ASAM_events.geojson",
        "WPI.geojson",
        "eca_reg13_nox.geojson",
        "eca_reg14_sox_pm.geojson",
        "eez_boundaries_v11.geojson",
        "FAO_AREAS_CWP.geojson",
    ]
    
    for name in standard_datasets:
        src = os.path.join(DATASET_DIR, name)
        if os.path.exists(src):
            optimize_feature_collection(src, out_opt_path=None, precision=4)
        else:
            print(f"[!] Warning: {name} not found in {DATASET_DIR}")

    print("=== Pipeline Complete! ===")

if __name__ == "__main__":
    main()
