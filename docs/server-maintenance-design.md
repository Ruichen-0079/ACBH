# Server Maintenance Design

ACBH V1 separates server maintenance decisions from host runtime execution.

## Source of truth

The Coordinator is the source of truth for maintenance state.

It records approved server-pack versions, world snapshots, admin-state versions, and future policy decisions about which artifacts are allowed to run.

## Agent role

The Agent is the executor.

It may download approved artifacts, restore local files, send heartbeat, and later run safe sync. It should not decide that local server-pack changes are globally accepted.

## Host role

The Host is only the current runtime node.

Being the current host does not grant permission to silently publish changes to mods, plugins, config, launch metadata, or other server-pack content.

## Artifact ownership

Server-pack changes should require explicit Owner/Admin approval in a later workflow. This includes server jars, mods, plugin jars, config, defaultconfigs, kubejs, scripts, and launch metadata.

World snapshots may be frequent because they represent runtime world data created by gameplay.

Admin state should be versioned separately from both server packs and world snapshots. This includes server properties, whitelist, ops, bans, permissions, and similar administrative files.

## V1 boundary

This document describes the maintenance model only. It does not implement scanning, approval workflow, RCON safe sync, process management, host election, or Minecraft integration.
