export interface BitrixMessageEvent {
  event: string;
  applicationToken: string;
  message: {
    text: string;
    authorId: number;
    isSystem: boolean;
  };
  chat: {
    dialogId: string;
  };
  bot: {
    id: number;
  };
}

export interface RagAnswer {
  answer(query: string, collections: string[]): Promise<string>;
}

export interface BitrixMessageSender {
  send(dialogId: string, message: string): Promise<void>;
}

export interface WebhookDependencies {
  rag: RagAnswer;
  bitrix: BitrixMessageSender;
  logger?: Pick<Console, "error">;
}
