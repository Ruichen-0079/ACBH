import { createHash, randomBytes, timingSafeEqual } from "node:crypto";
import type { ArtifactKind } from "./domain/artifacts.js";

export type MemberRole = "owner" | "member";

export type HostStatus = "online" | "standby" | "hosting" | "unhealthy" | "offline";

export type ArtifactStatus = "uploading" | "available" | "rejected";

export type ArtifactMetadata = {
  groupId: string;
  artifactKind: ArtifactKind;
  artifactId: string;
  parentArtifactId: string | null;
  serverPackVersion: string | null;
  creatorHostId: string;
  createdAt: string;
  updatedAt: string;
  status: ArtifactStatus;
  manifestSha256: string;
  manifestObjectPath: string;
  fileCount: number;
  totalBytes: number;
};

export type GroupState = {
  groupId: string;
  name: string;
  currentHostId: string | null;
  latestSnapshotId: string | null;
  createdAt: string;
  updatedAt: string;
  members: Array<{
    memberId: string;
    displayName: string;
    role: MemberRole;
    createdAt: string;
  }>;
  hosts: Array<{
    hostId: string;
    memberId: string;
    deviceName: string;
    platform: string;
    agentVersion: string;
    status: HostStatus;
    latestLocalSnapshotId: string | null;
    createdAt: string;
    updatedAt: string;
    lastHeartbeatAt: string | null;
  }>;
};

type MemberRecord = {
  memberId: string;
  displayName: string;
  role: MemberRole;
  createdAt: string;
};

type HostRecord = {
  hostId: string;
  memberId: string;
  deviceName: string;
  platform: string;
  agentVersion: string;
  status: HostStatus;
  hostTokenHash: string;
  latestLocalSnapshotId: string | null;
  createdAt: string;
  updatedAt: string;
  lastHeartbeatAt: string | null;
};

type GroupRecord = {
  groupId: string;
  name: string;
  accessKeyHash: string;
  currentHostId: string | null;
  latestSnapshotId: string | null;
  createdAt: string;
  updatedAt: string;
  members: Map<string, MemberRecord>;
  hosts: Map<string, HostRecord>;
  artifacts: Map<ArtifactKind, Map<string, ArtifactMetadata>>;
  latestArtifacts: Map<ArtifactKind, string>;
};

export class StoreError extends Error {
  constructor(
    public readonly statusCode: number,
    message: string,
  ) {
    super(message);
    this.name = "StoreError";
  }
}

export class InMemoryCoordinatorStore {
  private readonly groups = new Map<string, GroupRecord>();

  createGroup(input: { name: string; ownerName: string }): {
    groupId: string;
    ownerMemberId: string;
    accessKey: string;
  } {
    const now = new Date().toISOString();
    const groupId = createId("grp");
    const ownerMemberId = createId("mem");
    const accessKey = createSecret("ak");

    this.groups.set(groupId, {
      groupId,
      name: input.name,
      accessKeyHash: hashSecret(accessKey),
      currentHostId: null,
      latestSnapshotId: null,
      createdAt: now,
      updatedAt: now,
      members: new Map([
        [
          ownerMemberId,
          {
            memberId: ownerMemberId,
            displayName: input.ownerName,
            role: "owner",
            createdAt: now,
          },
        ],
      ]),
      hosts: new Map(),
      artifacts: new Map(),
      latestArtifacts: new Map(),
    });

    return { groupId, ownerMemberId, accessKey };
  }

  joinGroup(input: { groupId: string; accessKey: string; displayName: string }): {
    memberId: string;
    role: "member";
  } {
    const group = this.requireGroup(input.groupId);

    if (!verifySecret(input.accessKey, group.accessKeyHash)) {
      throw new StoreError(401, "Invalid access key");
    }

    const now = new Date().toISOString();
    const memberId = createId("mem");
    group.members.set(memberId, {
      memberId,
      displayName: input.displayName,
      role: "member",
      createdAt: now,
    });
    group.updatedAt = now;

    return { memberId, role: "member" };
  }

  registerHost(input: {
    groupId: string;
    memberId: string;
    deviceName: string;
    platform: string;
    agentVersion: string;
  }): { hostId: string; hostToken: string } {
    const group = this.requireGroup(input.groupId);

    if (!group.members.has(input.memberId)) {
      throw new StoreError(404, "Member does not exist in group");
    }

    const now = new Date().toISOString();
    const hostId = createId("host");
    const hostToken = createSecret("ht");
    group.hosts.set(hostId, {
      hostId,
      memberId: input.memberId,
      deviceName: input.deviceName,
      platform: input.platform,
      agentVersion: input.agentVersion,
      status: "standby",
      hostTokenHash: hashSecret(hostToken),
      latestLocalSnapshotId: null,
      createdAt: now,
      updatedAt: now,
      lastHeartbeatAt: now,
    });
    group.updatedAt = now;

    return { hostId, hostToken };
  }

  updateHeartbeat(input: {
    groupId: string;
    hostId: string;
    hostToken: string;
    status: HostStatus;
    latestLocalSnapshotId: string | null;
  }): { ok: true; hostId: string; status: HostStatus } {
    const group = this.requireGroup(input.groupId);
    const host = group.hosts.get(input.hostId);

    if (!host) {
      throw new StoreError(404, "Host does not exist in group");
    }

    if (!verifySecret(input.hostToken, host.hostTokenHash)) {
      throw new StoreError(401, "Invalid host token");
    }

    const now = new Date().toISOString();
    host.status = input.status;
    host.latestLocalSnapshotId = input.latestLocalSnapshotId;
    host.lastHeartbeatAt = now;
    host.updatedAt = now;
    group.updatedAt = now;

    return { ok: true, hostId: host.hostId, status: host.status };
  }

  getGroupState(groupId: string): GroupState {
    const group = this.requireGroup(groupId);

    return {
      groupId: group.groupId,
      name: group.name,
      currentHostId: group.currentHostId,
      latestSnapshotId: group.latestSnapshotId,
      createdAt: group.createdAt,
      updatedAt: group.updatedAt,
      members: Array.from(group.members.values()).map((member) => ({
        memberId: member.memberId,
        displayName: member.displayName,
        role: member.role,
        createdAt: member.createdAt,
      })),
      hosts: Array.from(group.hosts.values()).map((host) => ({
        hostId: host.hostId,
        memberId: host.memberId,
        deviceName: host.deviceName,
        platform: host.platform,
        agentVersion: host.agentVersion,
        status: host.status,
        latestLocalSnapshotId: host.latestLocalSnapshotId,
        createdAt: host.createdAt,
        updatedAt: host.updatedAt,
        lastHeartbeatAt: host.lastHeartbeatAt,
      })),
    };
  }

  verifyHost(input: { groupId: string; hostId: string; hostToken: string }): void {
    const group = this.requireGroup(input.groupId);
    const host = group.hosts.get(input.hostId);

    if (!host) {
      throw new StoreError(404, "Host does not exist in group");
    }

    if (!verifySecret(input.hostToken, host.hostTokenHash)) {
      throw new StoreError(401, "Invalid host token");
    }
  }

  recordArtifact(metadata: Omit<ArtifactMetadata, "updatedAt">): ArtifactMetadata {
    const group = this.requireGroup(metadata.groupId);

    if (metadata.status === "available" && metadata.artifactKind === "world-snapshot" && !metadata.serverPackVersion) {
      throw new StoreError(400, "serverPackVersion is required for world-snapshot artifacts");
    }

    const now = new Date().toISOString();
    const artifact = {
      ...metadata,
      updatedAt: now,
    };

    let artifactsByKind = group.artifacts.get(metadata.artifactKind);
    if (!artifactsByKind) {
      artifactsByKind = new Map();
      group.artifacts.set(metadata.artifactKind, artifactsByKind);
    }
    artifactsByKind.set(metadata.artifactId, artifact);

    if (artifact.status === "available" && this.shouldAdvanceLatest(group, artifact)) {
      group.latestArtifacts.set(artifact.artifactKind, artifact.artifactId);
      if (artifact.artifactKind === "world-snapshot") {
        group.latestSnapshotId = artifact.artifactId;
      }
    }

    group.updatedAt = now;
    return artifact;
  }

  listArtifacts(groupId: string, artifactKind?: ArtifactKind): ArtifactMetadata[] {
    const group = this.requireGroup(groupId);
    const maps = artifactKind ? [group.artifacts.get(artifactKind)] : Array.from(group.artifacts.values());

    return maps
      .filter((artifacts): artifacts is Map<string, ArtifactMetadata> => artifacts !== undefined)
      .flatMap((artifacts) => Array.from(artifacts.values()))
      .sort((a, b) => a.createdAt.localeCompare(b.createdAt) || a.artifactId.localeCompare(b.artifactId));
  }

  getArtifact(groupId: string, artifactKind: ArtifactKind, artifactId: string): ArtifactMetadata {
    const group = this.requireGroup(groupId);
    const artifact = group.artifacts.get(artifactKind)?.get(artifactId);

    if (!artifact) {
      throw new StoreError(404, "Artifact does not exist");
    }

    return artifact;
  }

  getLatestArtifact(groupId: string, artifactKind: ArtifactKind): ArtifactMetadata {
    const group = this.requireGroup(groupId);
    const artifactId = group.latestArtifacts.get(artifactKind);

    if (!artifactId) {
      throw new StoreError(404, "No available artifact exists for this kind");
    }

    return this.getArtifact(groupId, artifactKind, artifactId);
  }

  private requireGroup(groupId: string): GroupRecord {
    const group = this.groups.get(groupId);

    if (!group) {
      throw new StoreError(404, "Group does not exist");
    }

    return group;
  }

  private shouldAdvanceLatest(group: GroupRecord, artifact: ArtifactMetadata): boolean {
    const currentLatestId = group.latestArtifacts.get(artifact.artifactKind);
    if (!currentLatestId) {
      return true;
    }

    const currentLatest = group.artifacts.get(artifact.artifactKind)?.get(currentLatestId);
    if (!currentLatest) {
      return true;
    }

    return artifact.createdAt > currentLatest.createdAt;
  }
}

export function createInMemoryCoordinatorStore(): InMemoryCoordinatorStore {
  return new InMemoryCoordinatorStore();
}

function createId(prefix: string): string {
  return `${prefix}_${randomBytes(12).toString("base64url")}`;
}

function createSecret(prefix: string): string {
  return `${prefix}_${randomBytes(24).toString("base64url")}`;
}

function hashSecret(secret: string): string {
  return createHash("sha256").update(secret, "utf8").digest("hex");
}

function verifySecret(secret: string, expectedHash: string): boolean {
  const actual = Buffer.from(hashSecret(secret), "hex");
  const expected = Buffer.from(expectedHash, "hex");

  return actual.length === expected.length && timingSafeEqual(actual, expected);
}
