import type { FastifyInstance, FastifyReply, FastifyRequest } from "fastify";
import { z } from "zod";
import type { CoordinatorStorage } from "./storage/index.js";
import type { InMemoryCoordinatorStore } from "./store.js";
import { StoreError } from "./store.js";

const createGroupSchema = z.object({
  name: z.string().trim().min(1).max(120),
  ownerName: z.string().trim().min(1).max(80),
});

const joinGroupParamsSchema = z.object({
  groupId: z.string().min(1),
});

const joinGroupSchema = z.object({
  accessKey: z.string().min(1),
  displayName: z.string().trim().min(1).max(80),
});

const registerHostSchema = z.object({
  groupId: z.string().min(1),
  memberId: z.string().min(1),
  deviceName: z.string().trim().min(1).max(120),
  platform: z.string().trim().min(1).max(40),
  agentVersion: z.string().trim().min(1).max(40),
});

const heartbeatSchema = z.object({
  groupId: z.string().min(1),
  hostId: z.string().min(1),
  hostToken: z.string().min(1),
  status: z.enum(["online", "standby", "hosting", "unhealthy", "offline"]),
  latestLocalSnapshotId: z.string().min(1).nullable(),
});

const groupStateParamsSchema = z.object({
  groupId: z.string().min(1),
});

export async function registerRoutes(
  app: FastifyInstance,
  store: InMemoryCoordinatorStore,
  storage: CoordinatorStorage,
): Promise<void> {
  app.get("/health", async () => {
    return {
      ok: true,
      service: "acbh-coordinator",
      version: "0.1.0",
    };
  });

  app.get("/v1/info", async () => {
    return {
      name: "ACBH Coordinator",
      mode: "in-memory",
      v1NonGoal: "hot-migration",
      persistence: "none",
    };
  });

  app.get("/v1/storage/info", async () => {
    return storage.info();
  });

  app.post("/v1/groups", async (request, reply) => {
    const body = parseBody(createGroupSchema, request, reply);
    if (!body) {
      return reply;
    }

    return store.createGroup(body);
  });

  app.post("/v1/groups/:groupId/join", async (request, reply) => {
    const params = parseParams(joinGroupParamsSchema, request, reply);
    const body = parseBody(joinGroupSchema, request, reply);
    if (!params || !body) {
      return reply;
    }

    return handleStoreCall(reply, () =>
      store.joinGroup({
        groupId: params.groupId,
        accessKey: body.accessKey,
        displayName: body.displayName,
      }),
    );
  });

  app.post("/v1/hosts/register", async (request, reply) => {
    const body = parseBody(registerHostSchema, request, reply);
    if (!body) {
      return reply;
    }

    return handleStoreCall(reply, () => store.registerHost(body));
  });

  app.post("/v1/hosts/heartbeat", async (request, reply) => {
    const body = parseBody(heartbeatSchema, request, reply);
    if (!body) {
      return reply;
    }

    return handleStoreCall(reply, () => store.updateHeartbeat(body));
  });

  app.get("/v1/groups/:groupId/state", async (request, reply) => {
    const params = parseParams(groupStateParamsSchema, request, reply);
    if (!params) {
      return reply;
    }

    return handleStoreCall(reply, () => store.getGroupState(params.groupId));
  });
}

function parseBody<T extends z.ZodTypeAny>(
  schema: T,
  request: FastifyRequest,
  reply: FastifyReply,
): z.infer<T> | null {
  const result = schema.safeParse(request.body);

  if (!result.success) {
    reply.code(400).send({
      error: "Bad Request",
      message: "Invalid request body",
      issues: result.error.issues,
    });
    return null;
  }

  return result.data;
}

function parseParams<T extends z.ZodTypeAny>(
  schema: T,
  request: FastifyRequest,
  reply: FastifyReply,
): z.infer<T> | null {
  const result = schema.safeParse(request.params);

  if (!result.success) {
    reply.code(400).send({
      error: "Bad Request",
      message: "Invalid route parameters",
      issues: result.error.issues,
    });
    return null;
  }

  return result.data;
}

function handleStoreCall<T>(reply: FastifyReply, call: () => T): T | FastifyReply {
  try {
    return call();
  } catch (error) {
    if (error instanceof StoreError) {
      return reply.code(error.statusCode).send({
        error: statusText(error.statusCode),
        message: error.message,
      });
    }

    throw error;
  }
}

function statusText(statusCode: number): string {
  switch (statusCode) {
    case 401:
      return "Unauthorized";
    case 403:
      return "Forbidden";
    case 404:
      return "Not Found";
    default:
      return "Error";
  }
}
