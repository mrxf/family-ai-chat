import 'json_read.dart';

/// Public profile of an active household member returned by `/api/family/members`.
class FamilyMember {
  final String id, username, displayName, avatar;
  const FamilyMember({
    required this.id,
    required this.username,
    required this.displayName,
    this.avatar = '',
  });

  String get initial => displayName.isEmpty ? (username.isEmpty ? '?' : username[0]) : displayName.substring(0, 1);

  factory FamilyMember.fromJson(Map<String, dynamic> j) => FamilyMember(
        id: asStr(j['id']),
        username: asStr(j['username']),
        displayName: asStr(j['display_name']),
        avatar: asStr(j['avatar']),
      );

  Map<String, dynamic> toJson() => {
        'id': id,
        'username': username,
        'display_name': displayName,
        'avatar': avatar,
      };
}

/// One participant in a family shared conversation.
class ConversationMember {
  final String userId, username, displayName, avatar, role;
  final int addedAt;
  const ConversationMember({
    required this.userId,
    this.username = '',
    required this.displayName,
    this.avatar = '',
    this.role = 'member',
    this.addedAt = 0,
  });

  bool get isOwner => role == 'owner';

  factory ConversationMember.fromJson(Map<String, dynamic> j) => ConversationMember(
        userId: asStr(j['user_id']),
        username: asStr(j['username']),
        displayName: asStr(j['display_name']),
        avatar: asStr(j['avatar']),
        role: asStr(j['role']).isEmpty ? 'member' : asStr(j['role']),
        addedAt: asInt(j['added_at']),
      );

  Map<String, dynamic> toJson() => {
        'user_id': userId,
        if (username.isNotEmpty) 'username': username,
        'display_name': displayName,
        if (avatar.isNotEmpty) 'avatar': avatar,
        'role': role,
        if (addedAt > 0) 'added_at': addedAt,
      };
}

/// Snapshot metadata for a short-code conversation share.
class ConversationShare {
  final String id, code, creatorUserId, creatorName, creatorAvatar, sourceConversationId, title;
  final int messageCount, expiresAt, revokedAt, createdAt;
  final bool hasComponents;

  const ConversationShare({
    required this.id,
    required this.code,
    this.creatorUserId = '',
    this.creatorName = '',
    this.creatorAvatar = '',
    this.sourceConversationId = '',
    required this.title,
    this.messageCount = 0,
    this.hasComponents = false,
    this.expiresAt = 0,
    this.revokedAt = 0,
    this.createdAt = 0,
  });

  bool get isRevoked => revokedAt > 0;
  bool get isExpired => expiresAt > 0 && DateTime.now().millisecondsSinceEpoch > expiresAt;

  factory ConversationShare.fromJson(Map<String, dynamic> j) => ConversationShare(
        id: asStr(j['id']),
        code: asStr(j['code']),
        creatorUserId: asStr(j['creator_user_id']),
        creatorName: asStr(j['creator_name']),
        creatorAvatar: asStr(j['creator_avatar']),
        sourceConversationId: asStr(j['source_conversation_id']),
        title: asStr(j['title']),
        messageCount: asInt(j['message_count']),
        hasComponents: j['has_components'] == true,
        expiresAt: asInt(j['expires_at']),
        revokedAt: asInt(j['revoked_at']),
        createdAt: asInt(j['created_at']),
      );
}
