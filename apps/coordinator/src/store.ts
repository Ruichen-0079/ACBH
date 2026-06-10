import { createHash, randomBytes, timingSafeEqual } from "node:crypto";
import type { ArtifactKind } from "./domain/artifacts.js";

export type MemberRole = "owner" | "member";

export type HostStatus = "online" | "standby" | "hosting" | "unhealthy" | "offline";

export type ArtifactStatus = "uploading" | "available" | "rejected";

export type LatestLocalArtifacts = Partial<Record<ArtifactKind, string>>;

export type HostScoreHints = {
  cpuCores?: number;
  memoryTotalBytes?: number;
  diskFreeBytes?: number;
  javaAvailable?: boolean;
};

export type HostConnection = {
  host: string;
  port: number;
  network: string;
};

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

export type ElectionCandidate = {
  hostId: string;
  eligible: boolean;
  score: number;
  reasons: string[];
  latestLocalArtifacts: LatestLocalArtifacts;
  lastHeartbeatAt: string | null;
};

export type ElectionReason = "manual" | "heartbeat-timeout" | "no-current-host";

export type ElectionResult = {
  electionId: string;
  groupId: string;
  reason: ElectionReason;
  selectedHostId: string | null;
  currentHostGeneration: number;
  assignmentId: string | null;
  candidates: ElectionCandidate[];
  createdAt: string;
};

export type TakeoverAssignmentStatus =
  | "offered"
  | "accepted"
  | "completed"
  | "failed"
  | "expired"
  | "cancelled";

export type TakeoverAssignmentRecord = {
  assignmentId: string;
  groupId: string;
  hostId: string;
  reason: ElectionReason;
  status: TakeoverAssignmentStatus;
  takeoverTokenHash: string | null;
  currentHostGeneration: number;
  latestArtifactsAtAssignment: LatestLocalArtifacts;
  createdAt: string;
  expiresAt: string;
  acceptedAt: string | null;
  completedAt: string | null;
  failedAt: string | null;
  failureReason: string | null;
};

export type PublicTakeoverAssignment = Omit<TakeoverAssignmentRecord, "takeoverTokenHash">;

export type ElectionRunResponse = {
  ok: boolean;
  groupId: string;
  selectedHostId: string | null;
  candidates: ElectionCandidate[];
  election: ElectionResult;
  assignment: PublicTakeoverAssignment | null;
};

export type ElectionStatus = {
  groupId: string;
  currentHostId: string | null;
  currentHostGeneration: number;
  lastElection: ElectionResult | null;
  activeTakeoverAssignment: PublicTakeoverAssignment | null;
};

export type TakeoverPollResponse = {
  assignment:
    | (PublicTakeoverAssignment & {
        takeoverToken?: string;
      })
    | null;
};

export type GroupState = {
  groupId: string;
  name: string;
  currentHostId: string | null;
  currentHostGeneration: number;
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
    latestLocalArtifacts: LatestLocalArtifacts;
    hostScoreHints: HostScoreHints;
    connection: HostConnection | null;
    computedHostScore: number;
    recentFailureCount: number;
    manualPriority: number;
    lastElectionCandidateAt: string | null;
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
  latestLocalArtifacts: LatestLocalArtifacts;
  hostScoreHints: HostScoreHints;
  connection: HostConnection | null;
  computedHostScore: number;
  recentFailureCount: number;
  manualPriority: number;
  lastElectionCandidateAt: string | null;
  createdAt: string;
  updatedAt: string;
  lastHeartbeatAt: string | null;
};

type GroupRecord = {
  groupId: string;
  name: string;
  accessKeyHash: string;
  currentHostId: string | null;
  currentHostGeneration: number;
  latestSnapshotId: string | null;
  createdAt: string;
  updatedAt: string;
  members: Map<string, MemberRecord>;
  hosts: Map<string, HostRecord>;
  artifacts: Map<ArtifactKind, Map<string, ArtifactMetadata>>;
  latestArtifacts: Map<ArtifactKind, string>;
  lastElection: ElectionResult | null;
  activeTakeoverAssignmentId: string | null;
  takeoverAssignments: Map<string, TakeoverAssignmentRecord>;
};

type StoreOptions = {
  now?: () => Date;
  heartbeatTimeoutMs?: number;
  assignmentTtlMs?: number;
};

const defaultHeartbeatTimeoutMs = 30_000;
const defaultAssignmentTtlMs = 60_000;
const fourGiB = 4 * 1024 * 1024 * 1024;
const tenGiB = 10 * 1024 * 1024 * 1024;

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
  private readonly now: () => Date;
  readonly heartbeatTimeoutMs: number;
  readonly assignmentTtlMs: number;

  constructor(options: StoreOptions = {}) {
    this.now = options.now ?? (() => new Date());
    this.heartbeatTimeoutMs =
      options.heartbeatTimeoutMs ?? positiveEnvInteger("ACBH_HOST_HEARTBEAT_TIMEOUT_MS", defaultHeartbeatTimeoutMs);
    this.assignmentTtlMs =
      options.assignmentTtlMs ?? positiveEnvInteger("ACBH_TAKEOVER_ASSIGNMENT_TTL_MS", defaultAssignmentTtlMs);
  }

  createGroup(input: { name: string; ownerName: string }): {
    groupId: string;
    ownerMemberId: string;
    accessKey: string;
  } {
    const now = this.nowIso();
    const groupId = createId("grp");
    const ownerMemberId = createId("mem");
    const accessKey = createSecret("ak");

    this.groups.set(groupId, {
      groupId,
      name: input.name,
      accessKeyHash: hashSecret(accessKey),
      currentHostId: null,
      currentHostGeneration: 0,
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
      lastElection: null,
      activeTakeoverAssignmentId: null,
      takeoverAssignments: new Map(),
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

    const now = this.nowIso();
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

    const now = this.nowIso();
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
      latestLocalArtifacts: {},
      hostScoreHints: {},
      connection: null,
      computedHostScore: 0,
      recentFailureCount: 0,
      manualPriority: 0,
      lastElectionCandidateAt: null,
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
    latestLocalSnapshotId?: string | null;
    latestLocalArtifacts?: LatestLocalArtifacts;
    hostScoreHints?: HostScoreHints;
    connection?: HostConnection | null;
  }): { ok: true; hostId: string; status: HostStatus } {
    const group = this.requireGroup(input.groupId);
    const host = this.requireHost(group, input.hostId);
    this.verifyHostToken(host, input.hostToken);

    const now = this.nowIso();
    host.status = input.status;
    if (input.latestLocalSnapshotId !== undefined) {
      host.latestLocalSnapshotId = input.latestLocalSnapshotId;
    }
    if (input.latestLocalArtifacts !== undefined) {
      host.latestLocalArtifacts = { ...input.latestLocalArtifacts };
    }
    if (
      input.latestLocalSnapshotId !== undefined &&
      input.latestLocalArtifacts?.["world-snapshot"] === undefined
    ) {
      if (input.latestLocalSnapshotId === null) {
        delete host.latestLocalArtifacts["world-snapshot"];
      } else {
        host.latestLocalArtifacts["world-snapshot"] = input.latestLocalSnapshotId;
      }
    }
    if (input.hostScoreHints !== undefined) {
      host.hostScoreHints = { ...input.hostScoreHints };
    }
    if (input.connection !== undefined) {
      host.connection = input.connection === null ? null : { ...input.connection };
    }
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
      currentHostGeneration: group.currentHostGeneration,
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
        latestLocalArtifacts: { ...host.latestLocalArtifacts },
        hostScoreHints: { ...host.hostScoreHints },
        connection: host.connection === null ? null : { ...host.connection },
        computedHostScore: host.computedHostScore,
        recentFailureCount: host.recentFailureCount,
        manualPriority: host.manualPriority,
        lastElectionCandidateAt: host.lastElectionCandidateAt,
        createdAt: host.createdAt,
        updatedAt: host.updatedAt,
        lastHeartbeatAt: host.lastHeartbeatAt,
      })),
    };
  }

  evaluateCandidates(groupId: string): ElectionCandidate[] {
    const group = this.requireGroup(groupId);
    const evaluatedAt = this.now();
    const evaluatedAtIso = evaluatedAt.toISOString();
    const latestWorld = this.findLatestArtifact(group, "world-snapshot");
    const latestServerPack = this.findLatestArtifact(group, "server-pack");
    const latestAdminState = this.findLatestArtifact(group, "admin-state");

    const candidates = Array.from(group.hosts.values()).map((host): ElectionCandidate => {
      const reasons: string[] = [];
      const heartbeatFresh = isFresh(host.lastHeartbeatAt, evaluatedAt, this.heartbeatTimeoutMs);
      const hasLatestWorld =
        latestWorld === undefined ||
        host.latestLocalArtifacts["world-snapshot"] === latestWorld.artifactId ||
        host.latestLocalSnapshotId === latestWorld.artifactId;

      if (host.status !== "online" && host.status !== "standby") {
        reasons.push(`status-${host.status}`);
      }
      if (!heartbeatFresh) {
        reasons.push("stale-heartbeat");
      }
      if (!hasLatestWorld) {
        reasons.push("missing-latest-world-snapshot");
      }
      if (host.hostScoreHints.javaAvailable === false) {
        reasons.push("java-unavailable");
      }

      let score = 0;
      if (heartbeatFresh) score += 30;
      if (latestWorld !== undefined && hasLatestWorld) score += 25;
      if (
        latestServerPack !== undefined &&
        host.latestLocalArtifacts["server-pack"] === latestServerPack.artifactId
      ) {
        score += 10;
      }
      if (
        latestAdminState !== undefined &&
        host.latestLocalArtifacts["admin-state"] === latestAdminState.artifactId
      ) {
        score += 5;
      }
      if (host.hostScoreHints.javaAvailable === true) score += 10;
      if ((host.hostScoreHints.memoryTotalBytes ?? 0) >= fourGiB) score += 10;
      if ((host.hostScoreHints.diskFreeBytes ?? 0) >= tenGiB) score += 5;
      score += Math.min(Math.max(host.hostScoreHints.cpuCores ?? 0, 0), 8);
      score += host.manualPriority;
      score -= host.recentFailureCount * 10;

      host.computedHostScore = score;
      host.lastElectionCandidateAt = evaluatedAtIso;

      return {
        hostId: host.hostId,
        eligible: reasons.length === 0,
        score,
        reasons,
        latestLocalArtifacts: { ...host.latestLocalArtifacts },
        lastHeartbeatAt: host.lastHeartbeatAt,
      };
    });

    return candidates.sort(compareCandidates);
  }

  setHostManualPriority(groupId: string, hostId: string, manualPriority: number): void {
    const group = this.requireGroup(groupId);
    const host = this.requireHost(group, hostId);
    host.manualPriority = manualPriority;
    host.updatedAt = this.nowIso();
  }

  recordHostFailure(groupId: string, hostId: string): void {
    const group = this.requireGroup(groupId);
    const host = this.requireHost(group, hostId);
    host.recentFailureCount += 1;
    host.updatedAt = this.nowIso();
  }

  runElection(input: { groupId: string; reason: ElectionReason }): ElectionRunResponse {
    const group = this.requireGroup(input.groupId);
    this.cancelActiveAssignment(group);
    const candidates = this.evaluateCandidates(input.groupId);
    const selected = candidates.find((candidate) => candidate.eligible);
    const now = this.now();
    const nowIso = now.toISOString();
    let assignment: TakeoverAssignmentRecord | null = null;

    if (selected !== undefined) {
      assignment = {
        assignmentId: createId("takeover"),
        groupId: group.groupId,
        hostId: selected.hostId,
        reason: input.reason,
        status: "offered",
        takeoverTokenHash: null,
        currentHostGeneration: group.currentHostGeneration,
        latestArtifactsAtAssignment: Object.fromEntries(group.latestArtifacts.entries()),
        createdAt: nowIso,
        expiresAt: new Date(now.getTime() + this.assignmentTtlMs).toISOString(),
        acceptedAt: null,
        completedAt: null,
        failedAt: null,
        failureReason: null,
      };
      group.takeoverAssignments.set(assignment.assignmentId, assignment);
      group.activeTakeoverAssignmentId = assignment.assignmentId;
    }

    const election: ElectionResult = {
      electionId: createId("election"),
      groupId: group.groupId,
      reason: input.reason,
      selectedHostId: selected?.hostId ?? null,
      currentHostGeneration: group.currentHostGeneration,
      assignmentId: assignment?.assignmentId ?? null,
      candidates: candidates.map(copyCandidate),
      createdAt: nowIso,
    };
    group.lastElection = election;
    group.updatedAt = nowIso;

    return {
      ok: selected !== undefined,
      groupId: group.groupId,
      selectedHostId: selected?.hostId ?? null,
      candidates: candidates.map(copyCandidate),
      election: copyElection(election),
      assignment: assignment === null ? null : publicAssignment(assignment),
    };
  }

  checkElectionTimeout(groupId: string): {
    electionNeeded: boolean;
    election: ElectionRunResponse | null;
  } {
    const group = this.requireGroup(groupId);
    if (group.currentHostId === null) {
      return {
        electionNeeded: true,
        election: this.runElection({ groupId, reason: "no-current-host" }),
      };
    }

    const currentHost = this.requireHost(group, group.currentHostId);
    if (isFresh(currentHost.lastHeartbeatAt, this.now(), this.heartbeatTimeoutMs)) {
      return { electionNeeded: false, election: null };
    }

    currentHost.status = "unhealthy";
    currentHost.updatedAt = this.nowIso();
    return {
      electionNeeded: true,
      election: this.runElection({ groupId, reason: "heartbeat-timeout" }),
    };
  }

  getElectionStatus(groupId: string): ElectionStatus {
    const group = this.requireGroup(groupId);
    this.expireActiveAssignment(group);
    const active = this.activeAssignment(group);

    return {
      groupId: group.groupId,
      currentHostId: group.currentHostId,
      currentHostGeneration: group.currentHostGeneration,
      lastElection: group.lastElection === null ? null : copyElection(group.lastElection),
      activeTakeoverAssignment: active === null ? null : publicAssignment(active),
    };
  }

  pollTakeover(input: {
    groupId: string;
    hostId: string;
    hostToken: string;
    dryRun?: boolean;
  }): TakeoverPollResponse {
    const group = this.requireGroup(input.groupId);
    const host = this.requireHost(group, input.hostId);
    this.verifyHostToken(host, input.hostToken);
    this.expireActiveAssignment(group);
    const assignment = this.activeAssignment(group);
    if (assignment === null || assignment.hostId !== input.hostId) {
      return { assignment: null };
    }

    if (input.dryRun === true) {
      return { assignment: publicAssignment(assignment) };
    }

    if (assignment.status === "offered" && assignment.takeoverTokenHash === null) {
      const takeoverToken = createSecret("tt");
      assignment.takeoverTokenHash = hashSecret(takeoverToken);
      return {
        assignment: {
          ...publicAssignment(assignment),
          takeoverToken,
        },
      };
    }

    return { assignment: publicAssignment(assignment) };
  }

  acceptTakeover(input: TakeoverActionInput): PublicTakeoverAssignment {
    const { assignment } = this.requireTakeoverAction(input);
    if (assignment.status !== "offered") {
      throw new StoreError(409, `Takeover assignment cannot be accepted from status ${assignment.status}`);
    }
    if (Date.parse(assignment.expiresAt) <= this.now().getTime()) {
      this.expireAssignment(this.requireGroup(input.groupId), assignment);
      throw new StoreError(409, "Takeover assignment has expired");
    }

    assignment.status = "accepted";
    assignment.acceptedAt = this.nowIso();
    return publicAssignment(assignment);
  }

  completeTakeover(input: TakeoverActionInput): PublicTakeoverAssignment {
    const { group, host, assignment } = this.requireTakeoverAction(input);
    if (assignment.status !== "accepted") {
      throw new StoreError(409, `Takeover assignment cannot be completed from status ${assignment.status}`);
    }
    if (assignment.currentHostGeneration !== group.currentHostGeneration) {
      throw new StoreError(409, "Takeover assignment generation is stale");
    }

    const now = this.nowIso();
    assignment.status = "completed";
    assignment.completedAt = now;
    group.currentHostId = host.hostId;
    group.currentHostGeneration += 1;
    group.activeTakeoverAssignmentId = null;
    group.updatedAt = now;
    host.status = "hosting";
    host.updatedAt = now;
    return publicAssignment(assignment);
  }

  failTakeover(input: TakeoverActionInput & { failureReason: string }): PublicTakeoverAssignment {
    const { group, host, assignment } = this.requireTakeoverAction(input);
    if (assignment.status !== "offered" && assignment.status !== "accepted") {
      throw new StoreError(409, `Takeover assignment cannot fail from status ${assignment.status}`);
    }

    const now = this.nowIso();
    assignment.status = "failed";
    assignment.failedAt = now;
    assignment.failureReason = input.failureReason;
    group.activeTakeoverAssignmentId = null;
    group.updatedAt = now;
    host.recentFailureCount += 1;
    host.updatedAt = now;
    return publicAssignment(assignment);
  }

  getTakeoverAssignment(groupId: string, assignmentId: string): TakeoverAssignmentRecord {
    const group = this.requireGroup(groupId);
    const assignment = group.takeoverAssignments.get(assignmentId);
    if (!assignment) {
      throw new StoreError(404, "Takeover assignment does not exist");
    }
    return {
      ...assignment,
      latestArtifactsAtAssignment: { ...assignment.latestArtifactsAtAssignment },
    };
  }

  verifyHost(input: { groupId: string; hostId: string; hostToken: string }): void {
    const group = this.requireGroup(input.groupId);
    const host = this.requireHost(group, input.hostId);
    this.verifyHostToken(host, input.hostToken);
  }

  recordArtifact(metadata: Omit<ArtifactMetadata, "updatedAt">): ArtifactMetadata {
    const group = this.requireGroup(metadata.groupId);

    if (metadata.status === "available" && metadata.artifactKind === "world-snapshot" && !metadata.serverPackVersion) {
      throw new StoreError(400, "serverPackVersion is required for world-snapshot artifacts");
    }

    const now = this.nowIso();
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

  recordArtifactFromHost(input: {
    metadata: Omit<ArtifactMetadata, "updatedAt">;
    hostId: string;
    hostToken: string;
    currentHostGeneration?: number;
  }): ArtifactMetadata {
    const group = this.requireGroup(input.metadata.groupId);
    const host = this.requireHost(group, input.hostId);
    this.verifyHostToken(host, input.hostToken);

    if (group.currentHostId !== null) {
      if (input.hostId !== group.currentHostId) {
        throw new StoreError(403, "Only the current host may publish artifacts");
      }
      if (input.currentHostGeneration === undefined) {
        throw new StoreError(400, "Host generation header is required when a current host is set");
      }
      if (input.currentHostGeneration !== group.currentHostGeneration) {
        throw new StoreError(409, "Host generation is stale; current host may have changed");
      }
    }

    return this.recordArtifact(input.metadata);
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
    const artifact = this.findLatestArtifact(group, artifactKind);

    if (!artifact) {
      throw new StoreError(404, "No available artifact exists for this kind");
    }

    return artifact;
  }

  private requireGroup(groupId: string): GroupRecord {
    const group = this.groups.get(groupId);

    if (!group) {
      throw new StoreError(404, "Group does not exist");
    }

    return group;
  }

  private requireHost(group: GroupRecord, hostId: string): HostRecord {
    const host = group.hosts.get(hostId);
    if (!host) {
      throw new StoreError(404, "Host does not exist in group");
    }
    return host;
  }

  private verifyHostToken(host: HostRecord, hostToken: string): void {
    if (!verifySecret(hostToken, host.hostTokenHash)) {
      throw new StoreError(401, "Invalid host token");
    }
  }

  private findLatestArtifact(group: GroupRecord, artifactKind: ArtifactKind): ArtifactMetadata | undefined {
    const artifactId = group.latestArtifacts.get(artifactKind);
    return artifactId === undefined ? undefined : group.artifacts.get(artifactKind)?.get(artifactId);
  }

  private shouldAdvanceLatest(group: GroupRecord, artifact: ArtifactMetadata): boolean {
    const currentLatest = this.findLatestArtifact(group, artifact.artifactKind);
    return currentLatest === undefined || artifact.createdAt > currentLatest.createdAt;
  }

  private nowIso(): string {
    return this.now().toISOString();
  }

  private activeAssignment(group: GroupRecord): TakeoverAssignmentRecord | null {
    if (group.activeTakeoverAssignmentId === null) return null;
    return group.takeoverAssignments.get(group.activeTakeoverAssignmentId) ?? null;
  }

  private cancelActiveAssignment(group: GroupRecord): void {
    this.expireActiveAssignment(group);
    const active = this.activeAssignment(group);
    if (active !== null && (active.status === "offered" || active.status === "accepted")) {
      active.status = "cancelled";
    }
    group.activeTakeoverAssignmentId = null;
  }

  private expireActiveAssignment(group: GroupRecord): void {
    const active = this.activeAssignment(group);
    if (
      active !== null &&
      (active.status === "offered" || active.status === "accepted") &&
      Date.parse(active.expiresAt) <= this.now().getTime()
    ) {
      this.expireAssignment(group, active);
    }
  }

  private expireAssignment(group: GroupRecord, assignment: TakeoverAssignmentRecord): void {
    assignment.status = "expired";
    if (group.activeTakeoverAssignmentId === assignment.assignmentId) {
      group.activeTakeoverAssignmentId = null;
    }
  }

  private requireTakeoverAction(input: TakeoverActionInput): {
    group: GroupRecord;
    host: HostRecord;
    assignment: TakeoverAssignmentRecord;
  } {
    const group = this.requireGroup(input.groupId);
    const host = this.requireHost(group, input.hostId);
    this.verifyHostToken(host, input.hostToken);
    const assignment = group.takeoverAssignments.get(input.assignmentId);
    if (!assignment) {
      throw new StoreError(404, "Takeover assignment does not exist");
    }
    if (assignment.hostId !== host.hostId) {
      throw new StoreError(403, "Takeover assignment belongs to another host");
    }
    if (assignment.takeoverTokenHash === null || !verifySecret(input.takeoverToken, assignment.takeoverTokenHash)) {
      throw new StoreError(401, "Invalid takeover token");
    }
    return { group, host, assignment };
  }
}

export function createInMemoryCoordinatorStore(options: StoreOptions = {}): InMemoryCoordinatorStore {
  return new InMemoryCoordinatorStore(options);
}

type TakeoverActionInput = {
  groupId: string;
  hostId: string;
  hostToken: string;
  assignmentId: string;
  takeoverToken: string;
};

function copyCandidate(candidate: ElectionCandidate): ElectionCandidate {
  return {
    ...candidate,
    reasons: [...candidate.reasons],
    latestLocalArtifacts: { ...candidate.latestLocalArtifacts },
  };
}

function copyElection(election: ElectionResult): ElectionResult {
  return {
    ...election,
    candidates: election.candidates.map(copyCandidate),
  };
}

function publicAssignment(assignment: TakeoverAssignmentRecord): PublicTakeoverAssignment {
  const { takeoverTokenHash: _takeoverTokenHash, ...result } = assignment;
  return {
    ...result,
    latestArtifactsAtAssignment: { ...result.latestArtifactsAtAssignment },
  };
}

function compareCandidates(a: ElectionCandidate, b: ElectionCandidate): number {
  return (
    b.score - a.score ||
    compareHeartbeatDescending(a.lastHeartbeatAt, b.lastHeartbeatAt) ||
    compareAscending(a.hostId, b.hostId)
  );
}

function compareAscending(a: string, b: string): number {
  if (a < b) return -1;
  if (a > b) return 1;
  return 0;
}

function compareHeartbeatDescending(a: string | null, b: string | null): number {
  if (a === b) return 0;
  if (a === null) return 1;
  if (b === null) return -1;
  return b.localeCompare(a);
}

function isFresh(lastHeartbeatAt: string | null, now: Date, timeoutMs: number): boolean {
  if (lastHeartbeatAt === null) return false;
  const heartbeatMs = Date.parse(lastHeartbeatAt);
  return Number.isFinite(heartbeatMs) && now.getTime() - heartbeatMs <= timeoutMs;
}

function positiveEnvInteger(name: string, fallback: number): number {
  const raw = process.env[name];
  if (raw === undefined || raw.trim() === "") return fallback;
  const parsed = Number(raw);
  return Number.isSafeInteger(parsed) && parsed > 0 ? parsed : fallback;
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
