import 'package:flutter/material.dart';

class PageLoading extends StatelessWidget {
  const PageLoading({super.key});
  @override
  Widget build(BuildContext context) =>
      const Center(child: CircularProgressIndicator());
}
